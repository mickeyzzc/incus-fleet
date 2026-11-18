package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type schemaMigration struct {
	version    int
	statements []string
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
		statements: []string{
			`ALTER TABLE host_tasks ADD COLUMN lease_owner TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		version: 5,
		statements: []string{
			`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, expires_at BIGINT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		},
	},
}

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

func (s *SQLStore) applyMigration(ctx context.Context, migration schemaMigration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin migration %d: %w", migration.version, err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, statement := range migration.statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("store: migration %d: %w", migration.version, err)
		}
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`),
		migration.version, time.Now().UTC().UnixNano()); err != nil {
		return fmt.Errorf("store: record migration %d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migration %d: %w", migration.version, err)
	}

	return nil
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
