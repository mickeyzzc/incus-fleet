// Package store persists migration tasks.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	_ "modernc.org/sqlite"             // Pure-Go SQLite driver
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("store: not found")

// Migration is a persisted migration task.
type Migration struct {
	ID            string    `json:"id"`
	SourceCluster string    `json:"source_cluster"`
	TargetCluster string    `json:"target_cluster"`
	Instance      string    `json:"instance"`
	InstanceType  string    `json:"instance_type"`
	Mode          string    `json:"mode"`
	Live          bool      `json:"live"`
	Status        string    `json:"status"`
	Progress      string    `json:"progress"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Store is the persistence interface used by the migration service.
type Store interface {
	CreateMigration(ctx context.Context, migration *Migration) error
	UpdateMigration(ctx context.Context, migration *Migration) error
	GetMigration(ctx context.Context, id string) (*Migration, error)
	ListMigrations(ctx context.Context, limit int) ([]*Migration, error)
	MarkInterrupted(ctx context.Context) error
	CreateKeystone(ctx context.Context, key *Keystone) error
	ListKeystones(ctx context.Context) ([]*Keystone, error)
	GetKeystone(ctx context.Context, id string) (*Keystone, error)
	UpdateKeystone(ctx context.Context, key *Keystone) error
	DeleteKeystone(ctx context.Context, id string) error
	CreateScript(ctx context.Context, script *Script) error
	ListScripts(ctx context.Context) ([]*Script, error)
	GetScript(ctx context.Context, id string) (*Script, error)
	UpdateScript(ctx context.Context, script *Script) error
	DeleteScript(ctx context.Context, id string) error
	Ping(ctx context.Context) error
	Close() error
}

// SQLStore implements Store with SQLite or PostgreSQL.
type SQLStore struct {
	db     *sql.DB
	driver string
}

// Open connects to the configured database and applies the schema.
func Open(ctx context.Context, driver, dsn string) (*SQLStore, error) {
	var (
		db  *sql.DB
		err error
	)

	switch driver {
	case "sqlite":
		db, err = sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("store: open sqlite: %w", err)
		}

		// SQLite in-memory databases are per-connection. A single connection
		// keeps schema and data visible to every caller.
		db.SetMaxOpenConns(1)

		if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store: configure sqlite: %w", err)
		}

		if !strings.Contains(dsn, "mode=memory") && dsn != ":memory:" {
			if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("store: enable sqlite WAL: %w", err)
			}
		}
	case "postgres":
		db, err = sql.Open("pgx", dsn)
		if err != nil {
			return nil, fmt.Errorf("store: open postgres: %w", err)
		}
	default:
		return nil, fmt.Errorf("store: unsupported driver %q", driver)
	}

	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	s := &SQLStore{db: db, driver: driver}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}

func (s *SQLStore) rebind(query string) string {
	if s.driver != "postgres" {
		return query
	}

	var out strings.Builder
	out.Grow(len(query) + 8)

	arg := 1
	for _, r := range query {
		if r == '?' {
			fmt.Fprintf(&out, "$%d", arg)
			arg++
			continue
		}
		out.WriteRune(r)
	}

	return out.String()
}

// CreateMigration inserts a migration task.
func (s *SQLStore) CreateMigration(ctx context.Context, migration *Migration) error {
	const query = `INSERT INTO migrations
		(id, source_cluster, target_cluster, instance, instance_type, mode, live, status, progress, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, s.rebind(query), //nolint:gosec // query is a constant; rebind only swaps placeholders
		migration.ID,
		migration.SourceCluster,
		migration.TargetCluster,
		migration.Instance,
		migration.InstanceType,
		migration.Mode,
		boolToInt(migration.Live),
		migration.Status,
		migration.Progress,
		migration.Error,
		migration.CreatedAt.UnixNano(),
		migration.UpdatedAt.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("store: create migration: %w", err)
	}

	return nil
}

// UpdateMigration updates mutable migration fields.
func (s *SQLStore) UpdateMigration(ctx context.Context, migration *Migration) error {
	const query = `UPDATE migrations SET status = ?, progress = ?, error = ?, updated_at = ? WHERE id = ?`

	res, err := s.db.ExecContext(ctx, s.rebind(query), //nolint:gosec // query is a constant; rebind only swaps placeholders
		migration.Status,
		migration.Progress,
		migration.Error,
		migration.UpdatedAt.UnixNano(),
		migration.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update migration: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update migration rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	return nil
}

// GetMigration returns a migration by ID.
func (s *SQLStore) GetMigration(ctx context.Context, id string) (*Migration, error) {
	const query = `SELECT id, source_cluster, target_cluster, instance, instance_type, mode, live, status, progress, error, created_at, updated_at
		FROM migrations WHERE id = ?`

	row := s.db.QueryRowContext(ctx, s.rebind(query), id) //nolint:gosec // query is a constant; rebind only swaps placeholders
	migration, err := scanMigration(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get migration: %w", err)
	}

	return migration, nil
}

// ListMigrations returns the newest migration tasks.
func (s *SQLStore) ListMigrations(ctx context.Context, limit int) ([]*Migration, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	const query = `SELECT id, source_cluster, target_cluster, instance, instance_type, mode, live, status, progress, error, created_at, updated_at
		FROM migrations ORDER BY created_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, s.rebind(query), limit)
	if err != nil {
		return nil, fmt.Errorf("store: list migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	migrations := make([]*Migration, 0, limit)
	for rows.Next() {
		migration, err := scanMigration(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan migration: %w", err)
		}
		migrations = append(migrations, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list migrations rows: %w", err)
	}

	return migrations, nil
}

// MarkInterrupted fails tasks left running when the process stopped.
func (s *SQLStore) MarkInterrupted(ctx context.Context) error {
	const query = `UPDATE migrations
		SET status = 'failed', error = 'interrupted by server restart', updated_at = ?
		WHERE status IN ('pending', 'preflight', 'stopping', 'transferring', 'starting', 'verifying')`

	if _, err := s.db.ExecContext(ctx, s.rebind(query), time.Now().UTC().UnixNano()); err != nil {
		return fmt.Errorf("store: mark interrupted: %w", err)
	}

	return nil
}

// Ping checks database connectivity.
func (s *SQLStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close closes the database.
func (s *SQLStore) Close() error {
	return s.db.Close()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMigration(row rowScanner) (*Migration, error) {
	var (
		migration Migration
		live      int
		createdNS int64
		updatedNS int64
	)

	err := row.Scan(
		&migration.ID,
		&migration.SourceCluster,
		&migration.TargetCluster,
		&migration.Instance,
		&migration.InstanceType,
		&migration.Mode,
		&live,
		&migration.Status,
		&migration.Progress,
		&migration.Error,
		&createdNS,
		&updatedNS,
	)
	if err != nil {
		return nil, err
	}

	migration.Live = live != 0
	migration.CreatedAt = time.Unix(0, createdNS).UTC()
	migration.UpdatedAt = time.Unix(0, updatedNS).UTC()

	return &migration, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}

	return 0
}
