package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openSQLite(t *testing.T) *SQLStore {
	t.Helper()

	s, err := Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	return s
}

func testMigration(id string) *Migration {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	return &Migration{
		ID:            id,
		SourceCluster: "c1",
		TargetCluster: "c2",
		Instance:      "web-1",
		InstanceType:  "container",
		Mode:          "move",
		Live:          false,
		Status:        "pending",
		Progress:      "",
		Error:         "",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func TestSQLiteMigrationLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	m := testMigration("m1")
	if err := s.CreateMigration(ctx, m); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}

	got, err := s.GetMigration(ctx, "m1")
	if err != nil {
		t.Fatalf("GetMigration() error = %v", err)
	}
	if got.SourceCluster != m.SourceCluster || got.Live != m.Live || !got.CreatedAt.Equal(m.CreatedAt) {
		t.Fatalf("GetMigration() = %#v", got)
	}

	m.Status = "done"
	m.Progress = "complete"
	m.UpdatedAt = m.UpdatedAt.Add(time.Second)
	if err := s.UpdateMigration(ctx, m); err != nil {
		t.Fatalf("UpdateMigration() error = %v", err)
	}

	got, err = s.GetMigration(ctx, "m1")
	if err != nil {
		t.Fatalf("GetMigration() error = %v", err)
	}
	if got.Status != "done" || got.Progress != "complete" {
		t.Fatalf("updated migration = %#v", got)
	}

	m2 := testMigration("m2")
	m2.CreatedAt = m2.CreatedAt.Add(time.Minute)
	m2.UpdatedAt = m2.CreatedAt
	if err := s.CreateMigration(ctx, m2); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}

	list, err := s.ListMigrations(ctx, 10)
	if err != nil {
		t.Fatalf("ListMigrations() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "m2" {
		t.Fatalf("ListMigrations() = %#v", list)
	}

	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestGetMigrationNotFound(t *testing.T) {
	s := openSQLite(t)
	if _, err := s.GetMigration(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetMigration() error = %v", err)
	}
}

func TestUpdateMigrationNotFound(t *testing.T) {
	s := openSQLite(t)
	m := testMigration("missing")
	if err := s.UpdateMigration(context.Background(), m); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateMigration() error = %v", err)
	}
}

func TestMarkInterrupted(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	running := testMigration("running")
	running.Status = "transferring"
	if err := s.CreateMigration(ctx, running); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}

	done := testMigration("done")
	done.Status = "done"
	if err := s.CreateMigration(ctx, done); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}

	if err := s.MarkInterrupted(ctx); err != nil {
		t.Fatalf("MarkInterrupted() error = %v", err)
	}

	got, err := s.GetMigration(ctx, "running")
	if err != nil {
		t.Fatalf("GetMigration() error = %v", err)
	}
	if got.Status != "failed" || got.Error != "interrupted by server restart" {
		t.Fatalf("running migration = %#v", got)
	}

	got, err = s.GetMigration(ctx, "done")
	if err != nil {
		t.Fatalf("GetMigration() error = %v", err)
	}
	if got.Status != "done" {
		t.Fatalf("done migration = %#v", got)
	}
}

func TestListMigrationsLimit(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	for i := 0; i < 5; i++ {
		m := testMigration(string(rune('a' + i)))
		m.CreatedAt = m.CreatedAt.Add(time.Duration(i) * time.Second)
		m.UpdatedAt = m.CreatedAt
		if err := s.CreateMigration(ctx, m); err != nil {
			t.Fatalf("CreateMigration() error = %v", err)
		}
	}

	list, err := s.ListMigrations(ctx, 2)
	if err != nil {
		t.Fatalf("ListMigrations() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListMigrations() len = %d", len(list))
	}

	list, err = s.ListMigrations(ctx, 0)
	if err != nil {
		t.Fatalf("ListMigrations() error = %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("ListMigrations() default len = %d", len(list))
	}
}

func TestOpenUnsupportedDriver(t *testing.T) {
	if _, err := Open(context.Background(), "mysql", "dsn"); err == nil {
		t.Fatal("Open() expected error")
	}
}

func TestPostgresLifecycle(t *testing.T) {
	dsn := os.Getenv("INCUS_FLEET_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("INCUS_FLEET_TEST_POSTGRES_DSN not set")
	}

	ctx := context.Background()
	s, err := Open(ctx, "postgres", dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.db.ExecContext(ctx, "DELETE FROM migrations"); err != nil {
		t.Fatalf("clean migrations: %v", err)
	}

	m := testMigration("pg-1")
	if err := s.CreateMigration(ctx, m); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}

	got, err := s.GetMigration(ctx, "pg-1")
	if err != nil {
		t.Fatalf("GetMigration() error = %v", err)
	}
	if got.ID != m.ID || got.Live != m.Live {
		t.Fatalf("GetMigration() = %#v", got)
	}

	m.Status = "done"
	m.UpdatedAt = m.UpdatedAt.Add(time.Second)
	if err := s.UpdateMigration(ctx, m); err != nil {
		t.Fatalf("UpdateMigration() error = %v", err)
	}
}

func TestRebind(t *testing.T) {
	s := &SQLStore{driver: "postgres"}
	got := s.rebind("SELECT * FROM migrations WHERE id = ? AND status = ?")
	want := "SELECT * FROM migrations WHERE id = $1 AND status = $2"
	if got != want {
		t.Fatalf("rebind() = %q, want %q", got, want)
	}

	s.driver = "sqlite"
	if got := s.rebind("SELECT ?"); got != "SELECT ?" {
		t.Fatalf("sqlite rebind() = %q", got)
	}
}

func TestOpenFileSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fleet.db")
	s, err := Open(context.Background(), "sqlite", path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	m := testMigration("file-1")
	if err := s.CreateMigration(context.Background(), m); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}
}

func TestOpenFileSQLError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "fleet.db")
	if _, err := Open(context.Background(), "sqlite", path); err == nil {
		t.Fatal("Open() expected error")
	}
}

func TestOpenPostgresError(t *testing.T) {
	if _, err := Open(context.Background(), "postgres", "postgres://127.0.0.1:1/fleet?connect_timeout=1"); err == nil {
		t.Fatal("Open() expected postgres error")
	}
}

func TestStoreErrorsAfterClose(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	m := testMigration("closed")
	if err := s.CreateMigration(ctx, m); err == nil {
		t.Fatal("CreateMigration() expected error")
	}
	if err := s.UpdateMigration(ctx, m); err == nil {
		t.Fatal("UpdateMigration() expected error")
	}
	if _, err := s.GetMigration(ctx, "closed"); err == nil {
		t.Fatal("GetMigration() expected error")
	}
	if _, err := s.ListMigrations(ctx, 10); err == nil {
		t.Fatal("ListMigrations() expected error")
	}
	if err := s.MarkInterrupted(ctx); err == nil {
		t.Fatal("MarkInterrupted() expected error")
	}
	if err := s.Ping(ctx); err == nil {
		t.Fatal("Ping() expected error")
	}
}
