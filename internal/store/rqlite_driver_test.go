package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

// fakeRows yields a fixed row, letting the wrapper be tested without rqlite.
type fakeRows struct {
	row  []driver.Value
	done bool
}

func (r *fakeRows) Columns() []string { return []string{"a", "b", "c", "d"} }

func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.row)
	r.done = true
	return nil
}

func TestRqliteRowsNormalisesIntegralFloats(t *testing.T) {
	rows := &rqliteRows{Rows: &fakeRows{row: []driver.Value{
		float64(1795071707391408053), // nanosecond timestamp
		float64(42),                  // small integer
		float64(1.5),                 // genuine fractional value
		"text",                       // non-numeric untouched
	}}}

	dest := make([]driver.Value, 4)
	if err := rows.Next(dest); err != nil {
		t.Fatalf("Next() error = %v", err)
	}

	if v, ok := dest[0].(int64); !ok {
		t.Fatalf("timestamp value = %T, want int64", dest[0])
	} else if v == 0 {
		t.Fatal("timestamp value must be non-zero")
	}

	if v, ok := dest[1].(int64); !ok || v != 42 {
		t.Fatalf("small integer = %#v (%T), want int64(42)", dest[1], dest[1])
	}

	// A real fraction must stay a float so decimal columns keep working.
	if v, ok := dest[2].(float64); !ok || v != 1.5 {
		t.Fatalf("fractional value = %#v (%T), want float64(1.5)", dest[2], dest[2])
	}

	if v, ok := dest[3].(string); !ok || v != "text" {
		t.Fatalf("text value = %#v (%T)", dest[3], dest[3])
	}
}

func TestRqliteRowsPropagatesError(t *testing.T) {
	rows := &rqliteRows{Rows: &errRows{}}
	if err := rows.Next(make([]driver.Value, 1)); err == nil {
		t.Fatal("Next() expected error")
	}
}

type errRows struct{}

func (r *errRows) Columns() []string { return []string{"a"} }
func (r *errRows) Close() error      { return nil }
func (r *errRows) Next([]driver.Value) error {
	return io.ErrUnexpectedEOF
}

func TestRqliteDriverOpenForwards(t *testing.T) {
	d := &rqliteDriver{inner: fakeDriver{}}
	if _, err := d.Open("anything"); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
}

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return fakeConn{}, nil }

type fakeConn struct{}

func (fakeConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, io.EOF }

// The optional-interface forwarders must degrade to ErrSkip instead of
// panicking when the wrapped connection does not implement them.
func TestRqliteConnOptionalInterfacesFallBack(t *testing.T) {
	c := &rqliteConn{Conn: fakeConn{}}
	ctx := context.Background()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping() = %v", err)
	}
	if err := c.ResetSession(ctx); err != nil {
		t.Fatalf("ResetSession() = %v", err)
	}
	if !c.IsValid() {
		t.Fatal("IsValid() = false, want true")
	}
	if _, err := c.QueryContext(ctx, "SELECT 1", nil); !errors.Is(err, driver.ErrSkip) {
		t.Fatalf("QueryContext() = %v, want ErrSkip", err)
	}
	if _, err := c.ExecContext(ctx, "SELECT 1", nil); !errors.Is(err, driver.ErrSkip) {
		t.Fatalf("ExecContext() = %v, want ErrSkip", err)
	}
	if _, err := c.BeginTx(ctx, driver.TxOptions{}); !errors.Is(err, driver.ErrSkip) {
		t.Fatalf("BeginTx() = %v, want ErrSkip", err)
	}
	if _, err := c.PrepareContext(ctx, "SELECT 1"); !errors.Is(err, driver.ErrSkip) {
		t.Fatalf("PrepareContext() = %v, want ErrSkip", err)
	}
	if err := c.CheckNamedValue(&driver.NamedValue{}); !errors.Is(err, driver.ErrSkip) {
		t.Fatalf("CheckNamedValue() = %v, want ErrSkip", err)
	}
}

// A statement whose underlying driver lacks the context interfaces must also
// report ErrSkip rather than falling back to the deprecated methods.
func TestRqliteStmtOptionalInterfacesFallBack(t *testing.T) {
	s := &rqliteStmt{Stmt: plainStmt{}}
	ctx := context.Background()

	if _, err := s.QueryContext(ctx, nil); !errors.Is(err, driver.ErrSkip) {
		t.Fatalf("QueryContext() = %v, want ErrSkip", err)
	}
	if _, err := s.ExecContext(ctx, nil); !errors.Is(err, driver.ErrSkip) {
		t.Fatalf("ExecContext() = %v, want ErrSkip", err)
	}
}

// Statements that do implement the context interfaces must be wrapped so their
// rows still pass through the normaliser.
func TestRqliteStmtWrapsRows(t *testing.T) {
	s := &rqliteStmt{Stmt: ctxStmt{}}
	rows, err := s.QueryContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("QueryContext() error = %v", err)
	}
	if _, ok := rows.(*rqliteRows); !ok {
		t.Fatalf("QueryContext() rows = %T, want *rqliteRows", rows)
	}
	if _, err := s.ExecContext(context.Background(), nil); err != nil {
		t.Fatalf("ExecContext() error = %v", err)
	}
}

// plainStmt implements only the base driver.Stmt contract.
type plainStmt struct{}

func (plainStmt) Close() error                               { return nil }
func (plainStmt) NumInput() int                              { return -1 }
func (plainStmt) Exec([]driver.Value) (driver.Result, error) { return nil, io.EOF }
func (plainStmt) Query([]driver.Value) (driver.Rows, error)  { return nil, io.EOF }

// ctxStmt implements the context variants and returns a row to normalise.
type ctxStmt struct{}

func (ctxStmt) Close() error                               { return nil }
func (ctxStmt) NumInput() int                              { return -1 }
func (ctxStmt) Exec([]driver.Value) (driver.Result, error) { return nil, io.EOF }
func (ctxStmt) Query([]driver.Value) (driver.Rows, error)  { return nil, io.EOF }

func (ctxStmt) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{row: []driver.Value{float64(7), nil, nil, nil}}, nil
}

func (ctxStmt) ExecContext(context.Context, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

// fullConn implements every optional interface so the forwarding paths run.
type fullConn struct{ ctxStmt }

func (fullConn) Ping(context.Context) error               { return nil }
func (fullConn) ResetSession(context.Context) error       { return nil }
func (fullConn) IsValid() bool                            { return true }
func (fullConn) CheckNamedValue(*driver.NamedValue) error { return nil }
func (fullConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nopTx{}, nil
}
func (fullConn) PrepareContext(context.Context, string) (driver.Stmt, error) {
	return ctxStmt{}, nil
}
func (fullConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{row: []driver.Value{float64(9), nil, nil, nil}}, nil
}
func (fullConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (fullConn) Prepare(string) (driver.Stmt, error) { return ctxStmt{}, nil }
func (fullConn) Close() error                        { return nil }
func (fullConn) Begin() (driver.Tx, error)           { return nopTx{}, nil }

type nopTx struct{}

func (nopTx) Commit() error   { return nil }
func (nopTx) Rollback() error { return nil }

// Every forwarder should delegate when the wrapped connection supports it.
func TestRqliteConnForwardsToInner(t *testing.T) {
	c := &rqliteConn{Conn: fullConn{}}
	ctx := context.Background()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping() = %v", err)
	}
	if err := c.ResetSession(ctx); err != nil {
		t.Fatalf("ResetSession() = %v", err)
	}
	if !c.IsValid() {
		t.Fatal("IsValid() = false")
	}
	if err := c.CheckNamedValue(&driver.NamedValue{}); err != nil {
		t.Fatalf("CheckNamedValue() = %v", err)
	}
	tx, err := c.BeginTx(ctx, driver.TxOptions{})
	if err != nil {
		t.Fatalf("BeginTx() = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() = %v", err)
	}

	stmt, err := c.PrepareContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("PrepareContext() = %v", err)
	}
	if _, ok := stmt.(*rqliteStmt); !ok {
		t.Fatalf("PrepareContext() stmt = %T, want *rqliteStmt", stmt)
	}

	rows, err := c.QueryContext(ctx, "SELECT 1", nil)
	if err != nil {
		t.Fatalf("QueryContext() = %v", err)
	}
	if _, ok := rows.(*rqliteRows); !ok {
		t.Fatalf("QueryContext() rows = %T, want *rqliteRows", rows)
	}

	res, err := c.ExecContext(ctx, "UPDATE x", nil)
	if err != nil {
		t.Fatalf("ExecContext() = %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("RowsAffected() = %d", n)
	}
}

// A prepare error must surface unchanged.
func TestRqliteConnPrepareError(t *testing.T) {
	c := &rqliteConn{Conn: errPrepareConn{}}
	if _, err := c.PrepareContext(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("PrepareContext() expected an error")
	}
}

type errPrepareConn struct{}

func (errPrepareConn) Prepare(string) (driver.Stmt, error) { return nil, io.ErrClosedPipe }
func (errPrepareConn) Close() error                        { return nil }
func (errPrepareConn) Begin() (driver.Tx, error)           { return nil, io.EOF }
func (errPrepareConn) PrepareContext(context.Context, string) (driver.Stmt, error) {
	return nil, io.ErrClosedPipe
}
