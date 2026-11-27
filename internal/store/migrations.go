package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type schemaMigration struct {
	version    int
	statements []string
	// addColumns are applied only when the column is missing. SQLite has no
	// "ALTER TABLE ... ADD COLUMN IF NOT EXISTS", and on rqlite every statement
	// autocommits, so a crash between the ALTER and the version insert would
	// otherwise make the migration fail forever on retry.
	addColumns []addColumn
}

// addColumn declares one column addition. The column is probed before the ALTER
// so re-running a partially applied migration is safe on every driver.
type addColumn struct {
	table      string
	column     string
	definition string
}

const migrationsSchema = `
CREATE TABLE IF NOT EXISTS migrations (
	id TEXT PRIMARY KEY,
	source_cluster TEXT NOT NULL,
	target_cluster TEXT NOT NULL,
	instance TEXT NOT NULL,
	instance_type TEXT NOT NULL,
	mode TEXT NOT NULL,
	live INTEGER NOT NULL,
	status TEXT NOT NULL,
	progress TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL
)`

const migrationsIndex = `CREATE INDEX IF NOT EXISTS idx_migrations_created_at ON migrations(created_at DESC)`

var schemaMigrations = []schemaMigration{
	{
		version: 1,
		statements: []string{
			migrationsSchema,
			migrationsIndex,
			assetSchema(),
			clusterSchema,
		},
	},
	{
		version:    2,
		statements: []string{hostSchema},
	},
	{
		version: 3,
		statements: []string{
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_enrollment_tokens_hash ON enrollment_tokens(token_hash)`,
		},
	},
	{
		version: 4,
		addColumns: []addColumn{
			{table: "host_tasks", column: "lease_owner", definition: `TEXT NOT NULL DEFAULT ''`},
		},
	},
	{
		version: 5,
		statements: []string{
			`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, expires_at BIGINT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		},
	},
	{
		// owner records which fleet instance submitted the migration. It lets a
		// restarting instance fail only its own interrupted tasks instead of
		// clobbering migrations another instance is still running.
		version: 6,
		addColumns: []addColumn{
			{table: "migrations", column: "owner", definition: `TEXT NOT NULL DEFAULT ''`},
		},
		statements: []string{
			`CREATE INDEX IF NOT EXISTS idx_migrations_owner ON migrations(owner)`,
		},
	},
	{
		// Local accounts replace the single shared admin token. Sessions gain the
		// identity columns so any center can authorize a request without local
		// state; rows written by earlier versions default to empty.
		version: 7,
		addColumns: []addColumn{
			{table: "sessions", column: "user_id", definition: `TEXT NOT NULL DEFAULT ''`},
			{table: "sessions", column: "username", definition: `TEXT NOT NULL DEFAULT ''`},
			{table: "sessions", column: "role", definition: `TEXT NOT NULL DEFAULT ''`},
		},
		statements: []string{
			userSchema,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username)`,
		},
	},
	{
		// Reusable unattended-install answer configurations: named templates
		// with at most one default per installer profile.
		version:    8,
		statements: []string{answerProfileSchema()},
	},
	{
		// Cluster private overlay networks: one row per fleet-managed
		// bridge/VXLAN/DHCP/NAT topology inside a cluster.
		version:    9,
		statements: []string{overlaySchema},
	},
	{
		// API tokens with explicit scopes. They replace ad-hoc shared secrets for
		// machine callers and are the credential a plugin install receives.
		version:    10,
		statements: []string{apiTokenSchema, apiTokenIndexes},
	},
	{
		// Additional packages for unattended template installs, so operators do
		// not have to fall back to a raw answer file for a common need.
		version: 11,
		addColumns: []addColumn{
			{table: "answer_profiles", column: "packages", definition: `TEXT NOT NULL DEFAULT ''`},
		},
	},
	{
		// Display-name overrides apply to config-file clusters too. Keeping them
		// in the shared store means a rename survives restarts on every center.
		version: 12,
		statements: []string{
			`CREATE TABLE IF NOT EXISTS cluster_display_names (
				cluster TEXT PRIMARY KEY,
				display_name TEXT NOT NULL
			)`,
		},
	},
	{
		// Installed plugins are persisted so a one-click install survives a
		// restart without editing the fleet YAML configuration.
		version:    13,
		statements: []string{installedPluginSchema},
	},
	{
		// Plugin installs that deploy an instance need to remember where it
		// lives so uninstall can remove exactly that service.
		version: 14,
		addColumns: []addColumn{
			{table: "installed_plugins", column: "cluster", definition: `TEXT NOT NULL DEFAULT ''`},
			{table: "installed_plugins", column: "instance", definition: `TEXT NOT NULL DEFAULT ''`},
		},
	},
	{
		// Approved wiring overrides are durable: a restart must not silently
		// fall back to static configuration while a plugin is still installed.
		version:    15,
		statements: []string{pluginWiringSchema},
	},
}

const pluginWiringSchema = `
CREATE TABLE IF NOT EXISTS plugin_wiring (
	plugin_id TEXT NOT NULL,
	key TEXT NOT NULL,
	value TEXT NOT NULL DEFAULT '',
	default_value TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	approved INTEGER NOT NULL DEFAULT 1,
	updated_at BIGINT NOT NULL,
	PRIMARY KEY (plugin_id, key)
)`

const installedPluginSchema = `
CREATE TABLE IF NOT EXISTS installed_plugins (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT '',
	upstream TEXT NOT NULL,
	ui_path TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT 'viewer',
	enabled INTEGER NOT NULL DEFAULT 1,
	token_id TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL
)`

// userSchema holds local accounts. Usernames are compared case-insensitively by
// storing them lowercase.
const userSchema = `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	username TEXT NOT NULL,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL,
	disabled INTEGER NOT NULL DEFAULT 0,
	must_change_password INTEGER NOT NULL DEFAULT 0,
	failed_attempts INTEGER NOT NULL DEFAULT 0,
	locked_until BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL
)`

func (s *SQLStore) migrate(ctx context.Context) error {
	const schema = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at BIGINT NOT NULL
	)`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("store: create schema migrations table: %w", err)
	}

	applied := make(map[int]struct{}, len(schemaMigrations))
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("store: list schema migrations: %w", err)
	}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: scan schema migration: %w", err)
		}
		applied[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("store: list schema migrations rows: %w", err)
	}
	_ = rows.Close()

	for _, migration := range schemaMigrations {
		if _, ok := applied[migration.version]; ok {
			continue
		}
		if err := s.applyMigration(ctx, migration); err != nil {
			return err
		}
	}

	return nil
}

// applyMigration brings one schema version to completion.
//
// It is written to be safe when two fleet instances start at the same time and
// when a process dies part-way through, because gorqlite's database/sql
// transactions are no-ops: each statement autocommits and a rollback does
// nothing. Two rules therefore apply:
//
//  1. Every statement must be independently re-runnable: DDL uses IF NOT
//     EXISTS, and column additions are probed first.
//  2. The version row is written last. A crash mid-way leaves the version
//     unrecorded but the schema already consistent, so the retry simply runs
//     the same idempotent statements again. Recording the version first would
//     instead mark an incomplete schema as done.
//
// Concurrency is handled by the same idempotency: both instances may run the
// statements, and the version insert uses INSERT OR IGNORE so the loser is a
// no-op rather than a primary-key error.
func (s *SQLStore) applyMigration(ctx context.Context, migration schemaMigration) error {
	for _, column := range migration.addColumns {
		exists, err := s.columnExists(ctx, column.table, column.column)
		if err != nil {
			return fmt.Errorf("store: migration %d probe %s.%s: %w", migration.version, column.table, column.column, err)
		}
		if exists {
			continue
		}
		stmt := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, column.table, column.column, column.definition)
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			// A concurrent ALTER that won the race presents as a duplicate
			// column; the desired state is already reached.
			if isDuplicateColumn(err) {
				continue
			}
			return fmt.Errorf("store: migration %d add %s.%s: %w", migration.version, column.table, column.column, err)
		}
	}

	for _, statement := range migration.statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("store: migration %d: %w", migration.version, err)
		}
	}

	if _, err := s.db.ExecContext(ctx, s.rebind(`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, ?)`),
		migration.version, time.Now().UTC().UnixNano()); err != nil {
		return fmt.Errorf("store: record migration %d: %w", migration.version, err)
	}

	return nil
}

// columnExists reports whether table already has the named column. It uses the
// driver-neutral PRAGMA form, which rqlite and PostgreSQL both understand
// through the information_schema fallback below.
func (s *SQLStore) columnExists(ctx context.Context, table, column string) (bool, error) {
	if s.driver == "postgres" {
		const query = `SELECT COUNT(*) FROM information_schema.columns WHERE table_name = $1 AND column_name = $2`
		var count int
		if err := s.db.QueryRowContext(ctx, query, table, column).Scan(&count); err != nil {
			return false, err
		}
		return count > 0, nil
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table)) //nolint:gosec // table name is a compile-time constant
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return false, err
	}
	for rows.Next() {
		// PRAGMA table_info returns cid, name, type, notnull, dflt_value, pk.
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return false, err
		}
		nameIndex := -1
		for i, name := range columns {
			if name == "name" {
				nameIndex = i
				break
			}
		}
		if nameIndex < 0 {
			continue
		}
		if name, ok := values[nameIndex].(string); ok && name == column {
			return true, nil
		}
	}

	return false, rows.Err()
}

// isDuplicateColumn recognises the error SQLite returns when a column already
// exists, so a lost race is treated as success rather than a fatal error.
func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())

	return strings.Contains(message, "duplicate column") || strings.Contains(message, "already exists")
}

// SchemaVersion returns the highest applied migration version. It is used by
// tests and diagnostics.
func (s *SQLStore) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0, err
		}
		return 0, fmt.Errorf("store: schema version: %w", err)
	}

	return version, nil
}
