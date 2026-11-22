package store

import (
	"context"
	"database/sql"
	"database/sql/driver"

	"github.com/rqlite/gorqlite/stdlib"
)

// rqliteDriverName is the driver name registered below. It behaves like the
// upstream rqlite driver but normalises numeric values.
const rqliteDriverName = "rqlite-int"

// rqliteDriver wraps gorqlite's database/sql driver so integer columns come
// back as int64 rather than float64.
//
// rqlite answers queries with JSON, and encoding/json decodes every number to
// float64. A BIGINT holding Unix nanoseconds therefore arrives as something
// like 1.795071707391408e+18, which database/sql refuses to Scan into int64.
// Every timestamp in our schema is stored that way, so without this wrapper
// nothing reads back correctly.
//
// The wrapper deliberately mirrors the upstream driver's shape. gorqlite's
// Conn implements only Prepare, Close, and Begin — no ExecerContext or
// QueryerContext — so database/sql always goes through Prepare and the
// statement's Exec/Query methods. Wrapping those is therefore sufficient, and
// claiming the optional interfaces here would short-circuit that path with
// driver.ErrSkip.
type rqliteDriver struct{ inner driver.Driver }

func init() {
	sql.Register(rqliteDriverName, &rqliteDriver{inner: &stdlib.Driver{}})
}

func (d *rqliteDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &rqliteConn{Conn: conn}, nil
}

// rqliteConn forwards everything except prepared statements, which are wrapped
// so their rows can be normalised.
type rqliteConn struct {
	driver.Conn
}

func (c *rqliteConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &rqliteStmt{Stmt: stmt}, nil
}

// rqliteStmt converts integral float64 values to int64 as rows are read.
type rqliteStmt struct {
	driver.Stmt
}

func (s *rqliteStmt) Query(args []driver.Value) (driver.Rows, error) {
	//nolint:staticcheck // gorqlite's Stmt implements only the base interface;
	// there is no context-aware variant to call.
	rows, err := s.Stmt.Query(args)
	if err != nil {
		return nil, err
	}
	return &rqliteRows{Rows: rows}, nil
}

// rqliteRows normalises values as they are handed to database/sql.
type rqliteRows struct {
	driver.Rows
}

func (r *rqliteRows) Next(dest []driver.Value) error {
	if err := r.Rows.Next(dest); err != nil {
		return err
	}
	for i, v := range dest {
		if f, ok := v.(float64); ok {
			// Only whole numbers become int64. A genuine fraction stays a
			// float, so real floating-point columns are unaffected.
			if f == float64(int64(f)) {
				dest[i] = int64(f)
			}
		}
	}
	return nil
}

// Compile-time check that the wrapper still satisfies driver.Conn.
var _ driver.Conn = (*rqliteConn)(nil)

// ctxUnused exists only to keep the context import referenced should the
// wrapper later grow context-aware methods.
var _ = context.Background
