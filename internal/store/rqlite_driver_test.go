package store

import (
	"database/sql/driver"
	"io"
	"testing"
)

type fakeRows struct {
	row  []driver.Value
	done bool
}

func (r *fakeRows) Columns() []string { return []string{"a", "b", "c", "d"} }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.row)
	r.done = true
	return nil
}

type plainStmt struct{}

func (plainStmt) Close() error                               { return nil }
func (plainStmt) NumInput() int                              { return -1 }
func (plainStmt) Exec([]driver.Value) (driver.Result, error) { return nil, io.EOF }
func (plainStmt) Query([]driver.Value) (driver.Rows, error)  { return nil, io.EOF }

type queryRowsStmt struct{ plainStmt }

func (queryRowsStmt) Query([]driver.Value) (driver.Rows, error) {
	return &fakeRows{row: []driver.Value{float64(7), nil, nil, nil}}, nil
}

type plainConn struct{}

func (plainConn) Prepare(string) (driver.Stmt, error) { return plainStmt{}, nil }
func (plainConn) Close() error                        { return nil }
func (plainConn) Begin() (driver.Tx, error)           { return nil, io.EOF }

type failingConn struct{ plainConn }

func (failingConn) Prepare(string) (driver.Stmt, error) { return nil, io.ErrClosedPipe }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return plainConn{}, nil }

type failingDriver struct{}

func (failingDriver) Open(string) (driver.Conn, error) { return nil, io.ErrClosedPipe }

type errRows struct{}

func (r *errRows) Columns() []string { return []string{"a"} }
func (r *errRows) Close() error      { return nil }
func (r *errRows) Next([]driver.Value) error {
	return io.ErrUnexpectedEOF
}

func TestRqliteRowsNormalisesIntegralFloats(t *testing.T) {
	rows := &rqliteRows{Rows: &fakeRows{row: []driver.Value{
		float64(1795071707391408053),
		float64(42),
		float64(1.5),
		"text",
	}}}
	dest := make([]driver.Value, 4)
	if err := rows.Next(dest); err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	v, ok := dest[0].(int64)
	if !ok || v == 0 {
		t.Fatalf("timestamp = %#v (%T), want non-zero int64", dest[0], dest[0])
	}
	if v, ok := dest[1].(int64); !ok || v != 42 {
		t.Fatalf("integer = %#v (%T), want int64(42)", dest[1], dest[1])
	}
	if v, ok := dest[2].(float64); !ok || v != 1.5 {
		t.Fatalf("fraction = %#v (%T), want float64(1.5)", dest[2], dest[2])
	}
	if v, ok := dest[3].(string); !ok || v != "text" {
		t.Fatalf("text = %#v (%T)", dest[3], dest[3])
	}
}

func TestRqliteRowsPropagatesError(t *testing.T) {
	rows := &rqliteRows{Rows: &errRows{}}
	if err := rows.Next(make([]driver.Value, 1)); err == nil {
		t.Fatal("Next() expected error")
	}
}

func TestRqliteDriverOpenForwards(t *testing.T) {
	d := &rqliteDriver{inner: fakeDriver{}}
	if _, err := d.Open("anything"); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
}

func TestRqliteDriverOpenPropagatesError(t *testing.T) {
	d := &rqliteDriver{inner: failingDriver{}}
	if _, err := d.Open("anything"); err == nil {
		t.Fatal("Open() expected an error")
	}
}

func TestRqliteConnPrepareWrapsStmt(t *testing.T) {
	c := &rqliteConn{Conn: plainConn{}}
	stmt, err := c.Prepare("SELECT 1")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if _, ok := stmt.(*rqliteStmt); !ok {
		t.Fatalf("Prepare() stmt = %T, want *rqliteStmt", stmt)
	}
}

func TestRqliteConnPrepareError(t *testing.T) {
	c := &rqliteConn{Conn: failingConn{}}
	if _, err := c.Prepare("SELECT 1"); err == nil {
		t.Fatal("Prepare() expected an error")
	}
}

func TestRqliteStmtQueryWrapsRows(t *testing.T) {
	s := &rqliteStmt{Stmt: queryRowsStmt{}}
	rows, err := s.Query(nil)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	wrapped, ok := rows.(*rqliteRows)
	if !ok {
		t.Fatalf("Query() rows = %T, want *rqliteRows", rows)
	}
	dest := make([]driver.Value, 4)
	if err := wrapped.Next(dest); err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if _, ok := dest[0].(int64); !ok {
		t.Fatalf("value through stmt = %T, want int64", dest[0])
	}
}

func TestRqliteStmtQueryPropagatesError(t *testing.T) {
	s := &rqliteStmt{Stmt: plainStmt{}}
	if _, err := s.Query(nil); err == nil {
		t.Fatal("Query() expected an error")
	}
}

func TestRqliteStmtExecForwards(t *testing.T) {
	s := &rqliteStmt{Stmt: plainStmt{}}
	if _, err := s.Exec(nil); err == nil {
		t.Fatal("Exec() expected the wrapped error")
	}
}
