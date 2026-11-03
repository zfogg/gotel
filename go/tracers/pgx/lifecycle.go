package pgx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zfogg/gotel/go/outcome"
	gotelsql "github.com/zfogg/gotel/go/tracers/sql"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type acquireKey struct{}
type lifecycleQueryKey struct{}

func lifecycleContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, lifecycleQueryKey{}, true)
}

func (t *Tracer) lifecycle(ctx context.Context, name, operation string) (context.Context, spanState) {
	if gotelsql.TracingDisabled(ctx) {
		return ctx, spanState{}
	}
	ctx, span := t.tracer.Start(ctx, t.config.Namer.Name(name), trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		gotelsql.SystemKey.String(t.config.System), gotelsql.NamespaceKey.String(t.config.Namespace), gotelsql.OperationKey.String(operation)))
	return ctx, spanState{span, time.Now(), operation}
}

// TraceAcquireStart/End measure the full pgxpool wait, including failed/cancelled
// acquisitions. Configure pool.ConnConfig.Tracer with this tracer to install them.
func (t *Tracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	ctx, st := t.lifecycle(ctx, "PoolAcquire", "ACQUIRE")
	return context.WithValue(ctx, acquireKey{}, st)
}

func (t *Tracer) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	st := stateFrom(ctx, acquireKey{})
	if st.span != nil {
		st.span.SetAttributes(attribute.Float64("db.client.connection.acquire_wait_seconds", time.Since(st.started).Seconds()))
	}
	t.end(ctx, st, data.Err, -1)
}

// TraceRelease reports release as an independent instantaneous lifecycle span.
// pgx provides no caller context for this hook; it must not borrow unrelated TLS.
func (t *Tracer) TraceRelease(_ *pgxpool.Pool, data pgxpool.TraceReleaseData) {
	ctx, st := t.lifecycle(context.Background(), "PoolRelease", "RELEASE")
	if st.span != nil {
		st.span.SetAttributes(connectionAttributes(data.Conn)...)
	}
	t.end(ctx, st, nil, -1)
}

// Beginner is implemented by pgx connections, pools and transactions.
type Beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// TxBeginner starts native transactions with explicit driver transaction options.
type TxBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// Begin starts a real transaction and returns a pgx.Tx that instruments nested
// savepoints and completion. Queries retain their existing pgx tracing hooks.
func (t *Tracer) Begin(ctx context.Context, db Beginner) (pgx.Tx, error) {
	txCtx, lifetime := t.lifecycle(ctx, "Transaction", "TRANSACTION")
	spanCtx, st := t.lifecycle(txCtx, "BeginTransaction", "BEGIN")
	tx, err := db.Begin(lifecycleContext(spanCtx))
	t.end(spanCtx, st, err, -1)
	if err != nil {
		t.end(txCtx, lifetime, err, -1)
		return tx, err
	}
	return &transaction{Tx: tx, tracer: t, lifetime: lifetime}, nil
}

// BeginTx is Begin with native isolation/read-only options forwarded unchanged.
func (t *Tracer) BeginTx(ctx context.Context, db TxBeginner, options pgx.TxOptions) (pgx.Tx, error) {
	txCtx, lifetime := t.lifecycle(ctx, "Transaction", "TRANSACTION")
	spanCtx, st := t.lifecycle(txCtx, "BeginTransaction", "BEGIN")
	tx, err := db.BeginTx(lifecycleContext(spanCtx), options)
	t.end(spanCtx, st, err, -1)
	if err != nil {
		t.end(txCtx, lifetime, err, -1)
		return tx, err
	}
	return &transaction{Tx: tx, tracer: t, lifetime: lifetime}, nil
}

type transaction struct {
	pgx.Tx
	tracer     *Tracer
	nested     bool
	lifetime   spanState
	finished   atomic.Bool
	once       sync.Once
	failure    error
	failureCtx context.Context
}

// MarkTransactionOutcome records an owner's callback failure before manual
// cleanup, without changing the driver's result or cleanup context. Returns
// false for transactions not created by this tracer. pgx transaction ownership
// remains single-goroutine; call before Commit/Rollback.
func MarkTransactionOutcome(tx pgx.Tx, ctx context.Context, err error) bool {
	wrapped, ok := tx.(*transaction)
	if !ok || wrapped.finished.Load() {
		return false
	}
	wrapped.failureCtx, wrapped.failure = ctx, err
	return true
}

func (tx *transaction) context(ctx context.Context) context.Context {
	if tx.lifetime.span != nil && !tx.finished.Load() {
		return trace.ContextWithSpan(ctx, tx.lifetime.span)
	}
	return ctx
}

func (tx *transaction) finish(ctx context.Context, err error) {
	if tx.failure != nil {
		ctx, err = tx.failureCtx, tx.failure
	}
	tx.once.Do(func() { tx.finished.Store(true); tx.tracer.end(ctx, tx.lifetime, err, -1) })
}

func (tx *transaction) Begin(ctx context.Context) (pgx.Tx, error) {
	txCtx, lifetime := tx.tracer.lifecycle(tx.context(ctx), "SavepointTransaction", "SAVEPOINT")
	spanCtx, st := tx.tracer.lifecycle(txCtx, "Savepoint", "SAVEPOINT")
	nested, err := tx.Tx.Begin(lifecycleContext(spanCtx))
	tx.tracer.end(spanCtx, st, err, -1)
	if err != nil {
		tx.tracer.end(txCtx, lifetime, err, -1)
		return nested, err
	}
	return &transaction{Tx: nested, tracer: tx.tracer, nested: true, lifetime: lifetime}, nil
}

func (tx *transaction) Commit(ctx context.Context) error {
	if tx.finished.Load() {
		return tx.Tx.Commit(lifecycleContext(ctx))
	}
	name, op := "CommitTransaction", "COMMIT"
	if tx.nested {
		name, op = "ReleaseSavepoint", "RELEASE"
	}
	spanCtx, st := tx.tracer.lifecycle(tx.context(ctx), name, op)
	err := tx.Tx.Commit(lifecycleContext(spanCtx))
	tx.tracer.end(spanCtx, st, err, -1)
	tx.finish(ctx, err)
	return err
}

func (tx *transaction) Rollback(ctx context.Context) error {
	if tx.finished.Load() {
		return tx.Tx.Rollback(lifecycleContext(ctx))
	}
	name, op := "RollbackTransaction", "ROLLBACK"
	if tx.nested {
		name, op = "RollbackSavepoint", "ROLLBACK TO"
	}
	spanCtx, st := tx.tracer.lifecycle(tx.context(ctx), name, op)
	err := tx.Tx.Rollback(lifecycleContext(spanCtx))
	telemetryErr := err
	if errors.Is(err, pgx.ErrTxClosed) {
		telemetryErr = nil
	} // normal deferred cleanup after commit
	tx.tracer.end(spanCtx, st, telemetryErr, -1)
	tx.finish(ctx, telemetryErr)
	return err
}

func (tx *transaction) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return tx.Tx.Exec(tx.context(ctx), sql, args...)
}
func (tx *transaction) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return tx.Tx.Query(tx.context(ctx), sql, args...)
}
func (tx *transaction) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return tx.Tx.QueryRow(tx.context(ctx), sql, args...)
}
func (tx *transaction) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return tx.Tx.Prepare(tx.context(ctx), name, sql)
}
func (tx *transaction) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, source pgx.CopyFromSource) (int64, error) {
	return tx.Tx.CopyFrom(tx.context(ctx), table, columns, source)
}
func (tx *transaction) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return tx.Tx.SendBatch(tx.context(ctx), batch)
}

// WithinTransaction owns completion and bounded cleanup even when execute panics
// or its context is cancelled. A successful callback commits; errors roll back.
// The callback error is returned unchanged. Rollback after Commit emits no span.
func (t *Tracer) WithinTransaction(ctx context.Context, db Beginner, execute func(context.Context, pgx.Tx) error) (err error) {
	tx, err := t.Begin(ctx, db)
	if err != nil {
		return err
	}
	defer func() {
		panicked := recover()
		if wrapped, ok := tx.(*transaction); ok {
			wrapped.failureCtx, wrapped.failure = ctx, err
			if panicked != nil {
				wrapped.failure = errors.New("transaction callback panic")
			}
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
		if panicked != nil {
			panic(panicked)
		}
	}()
	if err = execute(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RetryConfig bounds application-owned transaction retries. Retryable must
// explicitly opt in; callbacks must roll back failed transactions before return.
type RetryConfig struct {
	// MaxAttempts includes initial execution and must be positive.
	MaxAttempts int
	// Retryable explicitly opts errors into retry; nil never retries.
	Retryable func(error) bool
	// Delay is the wait after the failed one-based attempt; nil/nonpositive skips waiting.
	Delay func(attempt int) time.Duration
}

// Retry preserves the last error and caller cancellation, and emits one span per
// attempt. It never guesses whether arbitrary operations are safe to repeat.
func (t *Tracer) Retry(ctx context.Context, config RetryConfig, execute func(context.Context, int) error) error {
	if config.MaxAttempts < 1 || execute == nil {
		return fmt.Errorf("positive attempts and execute are required")
	}
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		spanCtx, st := t.lifecycle(ctx, "TransactionAttempt", "TRANSACTION")
		if st.span != nil {
			st.span.SetAttributes(attribute.Int("db.transaction.attempt", attempt), attribute.Bool("db.transaction.retry", attempt > 1))
		}
		err := t.executeAttempt(spanCtx, st, execute, attempt)
		if err == nil || attempt >= config.MaxAttempts || config.Retryable == nil || !config.Retryable(err) || outcome.Classify(ctx, err) == outcome.Cancelled || ctx.Err() != nil {
			return err
		}
		delay := time.Duration(0)
		if config.Delay != nil {
			delay = config.Delay(attempt)
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}

func (t *Tracer) executeAttempt(ctx context.Context, st spanState, execute func(context.Context, int) error, attempt int) (err error) {
	defer func() {
		if panicked := recover(); panicked != nil {
			t.end(ctx, st, errors.New("transaction attempt panic"), -1)
			panic(panicked)
		}
		t.end(ctx, st, err, -1)
	}()
	return execute(ctx, attempt)
}

var _ pgxpool.AcquireTracer = (*Tracer)(nil)
var _ pgxpool.ReleaseTracer = (*Tracer)(nil)
var _ pgx.Tx = (*transaction)(nil)
