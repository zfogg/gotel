// Package gorm instruments GORM operations with stable names and
// sanitized SQL templates.
package gorm

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zfogg/gotel/go"
	"github.com/zfogg/gotel/go/internal/operationcontext"
	"github.com/zfogg/gotel/go/outcome"
	gotelsql "github.com/zfogg/gotel/go/tracers/sql"
	sequel "github.com/zfogg/sequel/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// WrapTransactions instruments connection acquisition and the BEGIN, COMMIT,
// and ROLLBACK lifecycle that GORM callbacks cannot observe.
func WrapTransactions(db *sql.DB, application gotel.Application, system ...string) gorm.ConnPool {
	database := "postgres"
	if len(system) > 0 {
		database = system[0]
	}
	return &transactionPool{DB: db, application: application, namer: gotelsql.StorageNamer(database)}
}

type transactionPool struct {
	*sql.DB
	application gotel.Application
	namer       gotelsql.Namer
}

func (p *transactionPool) GetDBConn() (*sql.DB, error) { return p.DB, nil }
func (p *transactionPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (pool gorm.ConnPool, err error) {
	tracer := otel.Tracer(strings.Trim(p.application.Prefix()+"/storage", "/"))
	txCtx, lifetime := tracer.Start(ctx, p.namer.Name("Transaction"), trace.WithSpanKind(trace.SpanKindClient))
	acquireCtx, acquire := tracer.Start(txCtx, p.namer.Name("PoolAcquire"), trace.WithSpanKind(trace.SpanKindClient))
	started := time.Now()
	conn, err := p.DB.Conn(acquireCtx)
	acquire.SetAttributes(attribute.Float64("db.client.connection.acquire_wait_seconds", time.Since(started).Seconds()))
	outcome.End(ctx, acquire, err)
	if err != nil {
		outcome.End(ctx, lifetime, err)
		return nil, err
	}
	beginCtx, span := tracer.Start(txCtx, p.namer.Name("BeginTransaction"), trace.WithSpanKind(trace.SpanKindClient))
	tx, err := conn.BeginTx(beginCtx, opts)
	outcome.End(ctx, span, err)
	if err != nil {
		_ = conn.Close()
		outcome.End(ctx, lifetime, err)
		return nil, err
	}
	return &tracedTransaction{Tx: tx, ctx: txCtx, parent: trace.SpanContextFromContext(ctx), db: p.DB, connection: conn, lifetime: lifetime, application: p.application, namer: p.namer}, nil
}

type tracedTransaction struct {
	*sql.Tx
	ctx         context.Context
	parent      trace.SpanContext
	db          *sql.DB
	application gotel.Application
	namer       gotelsql.Namer
	connection  *sql.Conn
	lifetime    trace.Span
	done        atomic.Bool
	once        sync.Once
	failure     error
	failureCtx  context.Context
}

// MarkTransactionOutcome records the owner's error before manually rolling back.
// It preserves native cleanup contexts/results and unwraps PreparedStmtTX.
func MarkTransactionOutcome(pool gorm.ConnPool, ctx context.Context, err error) bool {
	if prepared, ok := pool.(*gorm.PreparedStmtTX); ok {
		pool = prepared.Tx
	}
	tx, ok := pool.(*tracedTransaction)
	if !ok || tx.done.Load() {
		return false
	}
	tx.failureCtx, tx.failure = ctx, err
	return true
}

func (t *tracedTransaction) GetDBConn() (*sql.DB, error) { return t.db, nil }
func (t *tracedTransaction) traceContext(ctx context.Context) context.Context {
	if t.done.Load() {
		return ctx
	}
	current := trace.SpanContextFromContext(ctx)
	// GORM retains the context originally passed to Begin. Bind that ordinary
	// caller to the transaction, but respect an explicit operation (even when
	// it started before Begin) or a child span supplied later. In particular,
	// the driver wrapper must retain a query span already created by before.
	if current.IsValid() && (operationcontext.IsCurrent(ctx) || current.TraceID() != t.parent.TraceID() || current.SpanID() != t.parent.SpanID()) {
		return ctx
	}
	return trace.ContextWithSpan(ctx, t.lifetime)
}
func (t *tracedTransaction) finish(err error) {
	t.once.Do(func() {
		t.done.Store(true)
		if t.connection != nil {
			_ = t.connection.Close()
		}
		ctx := t.ctx
		if t.failure != nil {
			ctx, err = t.failureCtx, t.failure
		}
		if errors.Is(err, sql.ErrTxDone) && ctx.Err() != nil {
			err = ctx.Err()
		}
		outcome.End(ctx, t.lifetime, err)
	})
}
func (t *tracedTransaction) Commit() (err error) {
	if t.done.Load() {
		return t.Tx.Commit()
	}
	_, span := otel.Tracer(strings.Trim(t.application.Prefix()+"/storage", "/")).Start(t.ctx, t.namer.Name("CommitTransaction"), trace.WithSpanKind(trace.SpanKindClient))
	defer func() { outcome.End(t.ctx, span, err); t.finish(err) }()
	return t.Tx.Commit()
}
func (t *tracedTransaction) Rollback() (err error) {
	if t.done.Load() {
		return t.Tx.Rollback()
	}
	_, span := otel.Tracer(strings.Trim(t.application.Prefix()+"/storage", "/")).Start(t.ctx, t.namer.Name("RollbackTransaction"), trace.WithSpanKind(trace.SpanKindClient))
	defer func() { outcome.End(t.ctx, span, err); t.finish(err) }()
	return t.Tx.Rollback()
}

func (t *tracedTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(t.traceContext(ctx), query, args...)
}
func (t *tracedTransaction) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(t.traceContext(ctx), query, args...)
}
func (t *tracedTransaction) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(t.traceContext(ctx), query, args...)
}
func (t *tracedTransaction) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.Tx.PrepareContext(t.traceContext(ctx), query)
}

var _ gorm.ConnPoolBeginner = (*transactionPool)(nil)
var _ gorm.GetDBConnector = (*transactionPool)(nil)
var _ gorm.Tx = (*tracedTransaction)(nil)

// Config controls one GORM plugin instance.
type Config struct {
	// Name identifies the GORM plugin. It defaults to gotel:sql.
	Name string
	// Scope is the OpenTelemetry instrumentation scope.
	Scope string
	// Namer validates and prefixes every database span name.
	Namer gotelsql.Namer
	// System is db.system.name, such as postgresql or duckdb. When empty, the
	// GORM dialector name is used.
	System string
	// Namespace is the logical database name recorded as db.namespace.
	Namespace string
	// Caller may return a stable simple operation name when the context does not
	// carry one. gotelsql.CallerNamer constructs the common implementation.
	Caller func() string
	// Dialect selects SQL sanitization rules. It is inferred from System when
	// empty and otherwise falls back to generic SQL.
	Dialect gotelsql.Dialect
	// Sanitize customizes the safe SQL template policy. Dialect is filled from
	// the Dialect field when omitted.
	Sanitize gotelsql.SanitizeOptions
	// Analysis enables unified per-call budgets and optional bind descriptors.
	Analysis *sequel.AnalysisOptions
	// SpanKind defaults to client. INTERNAL is useful for embedded databases
	// such as DuckDB that execute in the application process.
	SpanKind trace.SpanKind
	// Observe receives bounded operation/result values and elapsed time after
	// each operation. It is suitable for application-owned metrics.
	Observe gotelsql.Observer
	// Outcome optionally classifies completion without changing db.Error.
	Outcome func(context.Context, error) outcome.Value
}

type plugin struct {
	config Config
	tracer trace.Tracer
}

// New returns a GORM plugin. Use db.Use(gotelgorm.New(config)) with an aliased tracer import.
func New(config Config) gorm.Plugin {
	if config.Name == "" {
		config.Name = "gotel:sql"
	}
	if config.Scope == "" {
		config.Scope = strings.ReplaceAll(strings.Trim(config.Namer.Prefix, "."), ".", "/")
	}
	if config.Dialect == "" {
		config.Dialect = gotelsql.Dialect(strings.ToLower(config.System))
	}
	if config.Sanitize.Dialect == "" {
		config.Sanitize = sequel.DefaultSanitizeOptions(config.Dialect)
	}
	if config.SpanKind == trace.SpanKindUnspecified {
		config.SpanKind = trace.SpanKindClient
	}
	return &plugin{config: config, tracer: otel.Tracer(config.Scope)}
}

func (p *plugin) Name() string { return p.config.Name }

func (p *plugin) Initialize(db *gorm.DB) error {
	if p.config.Namer.Prefix == "" {
		p.config.Namer = gotelsql.StorageNamer(p.system(db))
	}
	register := func(op string, before, after func(*gorm.DB)) error {
		key := "gotel:" + op
		switch op {
		case "query":
			return join(db.Callback().Query().Before("gorm:query").Register(key+":before", before), db.Callback().Query().After("gorm:query").Register(key+":after", after))
		case "create":
			return join(db.Callback().Create().Before("gorm:create").Register(key+":before", before), db.Callback().Create().After("gorm:create").Register(key+":after", after))
		case "update":
			return join(db.Callback().Update().Before("gorm:update").Register(key+":before", before), db.Callback().Update().After("gorm:update").Register(key+":after", after))
		case "delete":
			return join(db.Callback().Delete().Before("gorm:delete").Register(key+":before", before), db.Callback().Delete().After("gorm:delete").Register(key+":after", after))
		case "row":
			return join(db.Callback().Row().Before("gorm:row").Register(key+":before", before), db.Callback().Row().After("gorm:row").Register(key+":after", after))
		default:
			return join(db.Callback().Raw().Before("gorm:raw").Register(key+":before", before), db.Callback().Raw().After("gorm:raw").Register(key+":after", after))
		}
	}
	for _, op := range []string{"query", "create", "update", "delete", "row", "raw"} {
		if err := register(op, p.before(op), p.after(op)); err != nil {
			return err
		}
	}
	return nil
}

type state struct {
	span    trace.Span
	started time.Time
	op      string
	name    string
}

func (p *plugin) before(op string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		ctx := db.Statement.Context
		if ctx == nil {
			ctx = context.Background()
		}
		if gotelsql.TracingDisabled(ctx) {
			return
		}
		pool := db.Statement.ConnPool
		if prepared, ok := pool.(*gorm.PreparedStmtTX); ok {
			pool = prepared.Tx
		}
		if transaction, ok := pool.(*tracedTransaction); ok {
			ctx = transaction.traceContext(ctx)
		}
		name := gotelsql.NameFromContext(ctx)
		if name == "" && p.config.Caller != nil {
			if simple := p.config.Caller(); simple != "" {
				name = p.config.Namer.Name(simple)
			}
		}
		if name == "" {
			name = p.config.Namer.MustName(fallbackName(op))
		}
		ctx, span := p.tracer.Start(ctx, name, trace.WithSpanKind(p.config.SpanKind))
		db.Statement.Context = ctx
		db.InstanceSet(p.config.Name+":state", state{span: span, started: time.Now(), op: op, name: name})
	}
}

func (p *plugin) after(op string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		value, ok := db.InstanceGet(p.config.Name + ":state")
		if !ok {
			return
		}
		st, ok := value.(state)
		if !ok {
			return
		}
		var metadata gotelsql.Metadata
		if p.config.Analysis != nil {
			options := *p.config.Analysis
			if options.Context == nil {
				options.Context = db.Statement.Context
			}
			if options.Dialect == "" {
				options.Dialect = p.config.Dialect
			}
			metadata = gotelsql.InspectAnalysis(p.config.Namer, "", db.Statement.SQL.String(), db.Statement.Vars, options)
		} else {
			metadata = gotelsql.InspectWithOptions(p.config.Namer, "", db.Statement.SQL.String(), db.Statement.Vars, p.config.Sanitize)
		}
		// Preserve the name selected before GORM built SQL, but attach the final
		// sanitized template and actual bind count.
		attrs := metadata.Attributes(p.system(db), p.config.Namespace)
		if db.Statement.Table != "" {
			attrs = append(attrs, attribute.String("db.collection.name", db.Statement.Table))
		}
		if db.RowsAffected >= 0 {
			attrs = append(attrs, attribute.Int64("db.response.returned_rows", db.RowsAffected))
		}
		result := "ok"
		err := db.Error
		if err == gorm.ErrRecordNotFound {
			err = nil
			result = "not_found"
		} else if err != nil {
			result = "error"
		}
		st.span.SetAttributes(attrs...)
		classify := p.config.Outcome
		if classify == nil {
			classify = outcome.Classify
		}
		completion := classify(db.Statement.Context, err)
		outcome.Apply(st.span, completion, err)
		st.span.End()
		if completion != outcome.Success {
			result = string(completion)
		}
		if p.config.Observe != nil {
			p.config.Observe(db.Statement.Context, op, result, time.Since(st.started))
		}
	}
}

func fallbackName(op string) string {
	switch op {
	case "query":
		return "Query"
	case "create":
		return "Create"
	case "update":
		return "Update"
	case "delete":
		return "Delete"
	case "row":
		return "Row"
	case "raw":
		return "Raw"
	default:
		return "Unknown"
	}
}

func (p *plugin) system(db *gorm.DB) string {
	if p.config.System != "" {
		return p.config.System
	}
	if db != nil && db.Dialector != nil {
		return db.Dialector.Name()
	}
	return ""
}

func join(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
