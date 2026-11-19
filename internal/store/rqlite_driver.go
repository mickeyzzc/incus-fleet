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
// rqlite answers queries with JSON, and gorqlite's Rows.Next hands the decoded
// value straight to database/sql. encoding/json decodes every number to
// float64, so a BIGINT holding Unix nanoseconds arrives as something like
// 1.795071707391408e+18, which database/sql refuses to Scan into int64. Our
// schema stores every timestamp in that form, so without this wrapper every
// timestamp read fails. The wrapper is registered under its own name and only
// applied when the rqlite driver is selected, leaving the upstream driver
// untouched.
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

// rqliteConn forwards everything except query results, which are wrapped.
type rqliteConn struct {
	driver.Conn
}

// Ensure optional interfaces keep working through the wrapper.
var (
	_ driver.QueryerContext     = (*rqliteConn)(nil)
	_ driver.ExecerContext      = (*rqliteConn)(nil)
	_ driver.Pinger             = (*rqliteConn)(nil)
	_ driver.ConnBeginTx        = (*rqliteConn)(nil)
	_ driver.ConnPrepareContext = (*rqliteConn)(nil)
	_ driver.NamedValueChecker  = (*rqliteConn)(nil)
	_ driver.SessionResetter    = (*rqliteConn)(nil)
	_ driver.Validator          = (*rqliteConn)(nil)
)

func (c *rqliteConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if q, ok := c.Conn.(driver.QueryerContext); ok {
		rows, err := q.QueryContext(ctx, query, args)
		if err != nil {
			return nil, err
		}
		return &rqliteRows{Rows: rows}, nil
	}
	return nil, driver.ErrSkip
}

func (c *rqliteConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := c.Conn.(driver.ExecerContext); ok {
		return e.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *rqliteConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *rqliteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	b, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	return b.BeginTx(ctx, opts)
}

func (c *rqliteConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	p, ok := c.Conn.(driver.ConnPrepareContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	stmt, err := p.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &rqliteStmt{Stmt: stmt}, nil
}

// rqliteStmt wraps prepared statements so their rows are normalised too.
// database/sql prefers the prepared-statement path for queries, so wrapping
// only Conn.QueryContext is not enough.
type rqliteStmt struct {
	driver.Stmt
}

var (
	_ driver.StmtQueryContext = (*rqliteStmt)(nil)
	_ driver.StmtExecContext  = (*rqliteStmt)(nil)
)

func (s *rqliteStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := s.Stmt.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := q.QueryContext(ctx, args)
	if err != nil {
		return nil, err
	}
	return &rqliteRows{Rows: rows}, nil
}

func (s *rqliteStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	e, ok := s.Stmt.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return e.ExecContext(ctx, args)
}

func (c *rqliteConn) CheckNamedValue(nv *driver.NamedValue) error {
	if ck, ok := c.Conn.(driver.NamedValueChecker); ok {
		return ck.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

func (c *rqliteConn) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *rqliteConn) IsValid() bool {
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

// rqliteRows converts integral float64 values to int64 as rows are read.
type rqliteRows struct {
	driver.Rows
}

func (r *rqliteRows) Next(dest []driver.Value) error {
	if err := r.Rows.Next(dest); err != nil {
		return err
	}
	for i, v := range dest {
		if f, ok := v.(float64); ok {
			// Only whole numbers become int64. A real fractional value stays a
			// float, so genuine floating-point columns are unaffected.
			if f == float64(int64(f)) {
				dest[i] = int64(f)
			}
		}
	}
	return nil
}
