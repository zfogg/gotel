package sqltrace

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

// QueryContextExecutor is implemented by *sql.DB, *sql.Conn, and *sql.Tx.
type QueryContextExecutor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// QueryRowContextExecutor is implemented by *sql.DB, *sql.Conn, and *sql.Tx.
type QueryRowContextExecutor interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ExecContextExecutor is implemented by *sql.DB, *sql.Conn, and *sql.Tx.
type ExecContextExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Rows wraps sql.Rows and completes its database span on exhaustion, scan
// failure, or Close. It counts rows successfully advanced by Next.
type Rows struct {
	*sql.Rows
	ctx   context.Context
	span  *DBSpan
	once  sync.Once
	count int64
}

// Next advances the underlying rows and completes telemetry at EOF or error.
func (r *Rows) Next() bool {
	if r == nil || r.Rows == nil {
		return false
	}
	ok := r.Rows.Next()
	if ok {
		r.count++
		return true
	}
	r.finish(r.Rows.Err())
	return false
}

// Scan delegates to sql.Rows.Scan and completes telemetry on scan failure.
func (r *Rows) Scan(dest ...any) error {
	if r == nil || r.Rows == nil {
		return sql.ErrNoRows
	}
	err := r.Rows.Scan(dest...)
	if err != nil {
		r.finish(err)
	}
	return err
}

// Close closes the underlying rows and completes telemetry exactly once.
func (r *Rows) Close() error {
	if r == nil || r.Rows == nil {
		return nil
	}
	err := r.Rows.Close()
	r.finish(err)
	return err
}

func (r *Rows) finish(err error) {
	if r == nil {
		return
	}
	r.once.Do(func() { r.span.End(r.ctx, err, r.count) })
}

// Row wraps sql.Row and completes its database span when Scan is called.
type Row struct {
	row  *sql.Row
	ctx  context.Context
	span *DBSpan
	once sync.Once
}

// Scan delegates to sql.Row.Scan and records zero returned rows for ErrNoRows.
func (r *Row) Scan(dest ...any) error {
	if r == nil || r.row == nil {
		return sql.ErrNoRows
	}
	err := r.row.Scan(dest...)
	rows := int64(1)
	if errors.Is(err, sql.ErrNoRows) {
		rows = 0
	} else if err != nil {
		rows = -1
	}
	r.once.Do(func() { r.span.End(r.ctx, err, rows) })
	return err
}

// QueryContext executes a database/sql query with automatic span lifecycle,
// safe query metadata, error recording, and returned-row counting.
func QueryContext(ctx context.Context, executor QueryContextExecutor, config SpanConfig, statement string, args ...any) (*Rows, error) {
	config.Statement = statement
	config.Args = args
	spanCtx, span := StartSpan(ctx, config)
	rows, err := executor.QueryContext(spanCtx, statement, args...)
	if err != nil {
		span.End(spanCtx, err, -1)
		return nil, err
	}
	return &Rows{Rows: rows, ctx: spanCtx, span: span}, nil
}

// QueryRowContext starts telemetry for a single-row database/sql query. The
// returned Row must be scanned so the operation and span can complete.
func QueryRowContext(ctx context.Context, executor QueryRowContextExecutor, config SpanConfig, statement string, args ...any) *Row {
	config.Statement = statement
	config.Args = args
	spanCtx, span := StartSpan(ctx, config)
	return &Row{row: executor.QueryRowContext(spanCtx, statement, args...), ctx: spanCtx, span: span}
}

// ExecContext executes a database/sql statement and records affected rows.
func ExecContext(ctx context.Context, executor ExecContextExecutor, config SpanConfig, statement string, args ...any) (sql.Result, error) {
	config.Statement = statement
	config.Args = args
	spanCtx, span := StartSpan(ctx, config)
	result, err := executor.ExecContext(spanCtx, statement, args...)
	rows := int64(-1)
	if err == nil && result != nil {
		if affected, rowsErr := result.RowsAffected(); rowsErr == nil {
			rows = affected
		}
	}
	span.End(spanCtx, err, rows)
	return result, err
}
