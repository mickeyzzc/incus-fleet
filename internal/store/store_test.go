package store

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

	if err := s.MarkInterrupted(ctx, ""); err != nil {
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

func TestMarkInterruptedScopedToOwner(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)

	mine := testMigration("mine")
	mine.Status = "transferring"
	mine.Owner = "instance-a"
	if err := s.CreateMigration(ctx, mine); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}

	theirs := testMigration("theirs")
	theirs.Status = "transferring"
	theirs.Owner = "instance-b"
	if err := s.CreateMigration(ctx, theirs); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}

	// Restarting instance-a must recover its own work only; instance-b's task
	// is still running over there.
	if err := s.MarkInterrupted(ctx, "instance-a"); err != nil {
		t.Fatalf("MarkInterrupted() error = %v", err)
	}

	got, err := s.GetMigration(ctx, "mine")
	if err != nil {
		t.Fatalf("GetMigration(mine) error = %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("own in-flight migration = %q, want failed", got.Status)
	}

	got, err = s.GetMigration(ctx, "theirs")
	if err != nil {
		t.Fatalf("GetMigration(theirs) error = %v", err)
	}
	if got.Status != "transferring" {
		t.Fatalf("other instance migration = %q, want untouched", got.Status)
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

func TestRqliteLifecycle(t *testing.T) {
	dsn := os.Getenv("INCUS_FLEET_TEST_RQLITE_URL")
	if dsn == "" {
		t.Skip("INCUS_FLEET_TEST_RQLITE_URL not set")
	}

	ctx := context.Background()
	s, err := Open(ctx, "rqlite", dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.db.ExecContext(ctx, "DELETE FROM migrations"); err != nil {
		t.Fatalf("clean migrations: %v", err)
	}

	m := testMigration("rq-1")
	if err := s.CreateMigration(ctx, m); err != nil {
		t.Fatalf("CreateMigration() error = %v", err)
	}

	got, err := s.GetMigration(ctx, "rq-1")
	if err != nil {
		t.Fatalf("GetMigration() error = %v", err)
	}
	if got.ID != m.ID || got.Live != m.Live || got.Owner != m.Owner {
		t.Fatalf("GetMigration() = %#v", got)
	}

	m.Status = "done"
	m.UpdatedAt = m.UpdatedAt.Add(time.Second)
	if err := s.UpdateMigration(ctx, m); err != nil {
		t.Fatalf("UpdateMigration() error = %v", err)
	}

	// The claim path is the one place that must not rely on transaction
	// semantics, since gorqlite's database/sql transactions are no-ops.
	now := time.Now().UTC()
	if err := s.CreateHostTask(ctx, &HostTask{ID: "rq-task", Cluster: "c1", Member: "m1",
		Type: "x", State: HostTaskQueued, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateHostTask() error = %v", err)
	}
	claimed, err := s.ClaimHostTask(ctx, "rq-worker", time.Minute)
	if err != nil || claimed.ID != "rq-task" || claimed.LeaseOwner != "rq-worker" {
		t.Fatalf("ClaimHostTask() = %#v, %v", claimed, err)
	}
	if _, err := s.ClaimHostTask(ctx, "rq-other", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second claim error = %v", err)
	}
}

func TestOpenRqliteError(t *testing.T) {
	if _, err := Open(context.Background(), "rqlite", "http://127.0.0.1:1"); err == nil {
		t.Fatal("Open() expected error")
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

	// rqlite speaks SQLite SQL, so placeholders must pass through untouched.
	s.driver = "rqlite"
	if got := s.rebind("SELECT * FROM migrations WHERE id = ?"); got != "SELECT * FROM migrations WHERE id = ?" {
		t.Fatalf("rqlite rebind() = %q", got)
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
	if err := s.MarkInterrupted(ctx, ""); err == nil {
		t.Fatal("MarkInterrupted() expected error")
	}
	if err := s.Ping(ctx); err == nil {
		t.Fatal("Ping() expected error")
	}
}

// TestNoUnboundPlaceholders guards the PostgreSQL path. pgx needs $n
// placeholders, and rebind() performs the conversion. Any call that passes a
// SQL string containing '?' without routing it through rebind breaks postgres
// at runtime, so fail the build instead.
//
// Queries are usually held in package-level or local constants, so this walks
// the AST, collects every string literal assigned to a name, and then checks
// each database call. A literal that is handed to rebind() is exempt.
func TestNoUnboundPlaceholders(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	queryMethods := map[string]bool{
		"ExecContext":     true,
		"QueryContext":    true,
		"QueryRowContext": true,
		"Exec":            true,
		"Query":           true,
		"QueryRow":        true,
	}

	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		// Map every declared name to its literal value, so a call passing a
		// variable resolves to the SQL text it holds.
		literals := map[string]string{}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.ValueSpec:
				for i, id := range n.Names {
					if i < len(n.Values) {
						if value := literalValue(n.Values[i]); value != "" {
							literals[id.Name] = value
						}
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					if i >= len(n.Rhs) {
						continue
					}
					id, ok := lhs.(*ast.Ident)
					if !ok {
						continue
					}
					if value := literalValue(n.Rhs[i]); value != "" {
						literals[id.Name] = value
					}
				}
			}
			return true
		})

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !queryMethods[selector.Sel.Name] {
				return true
			}
			inner, ok := selector.X.(*ast.SelectorExpr)
			if !ok || inner.Sel.Name != "db" {
				return true
			}
			if len(call.Args) < 2 {
				return true
			}

			// A call with no arguments after the query has nothing to bind, so
			// '?' inside it cannot be a placeholder (it is part of a literal or
			// a comment in the SQL text).
			if len(call.Args) == 2 {
				return true
			}

			queryArg := call.Args[1]

			// s.rebind(...) is the sanctioned path.
			if wrapped, ok := queryArg.(*ast.CallExpr); ok {
				if fn, ok := wrapped.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "rebind" {
					return true
				}
			}

			value := literalValue(queryArg)
			if value == "" {
				if id, ok := queryArg.(*ast.Ident); ok {
					value = literals[id.Name]
				}
			}

			if strings.Contains(value, "?") {
				t.Errorf("%s: parameterized query bypasses rebind at line %d",
					name, fset.Position(call.Pos()).Line)
			}

			return true
		})
	}
}

// literalValue extracts the contents of a string literal argument, returning
// an empty string for anything that is not a plain literal.
func literalValue(expr ast.Expr) string {
	basic, ok := expr.(*ast.BasicLit)
	if !ok || basic.Kind != token.STRING {
		return ""
	}

	value, err := strconv.Unquote(basic.Value)
	if err != nil {
		return ""
	}

	return value
}
