package pgx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zfogg/gotel/go/outcome"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type fakeTx struct {
	pgx.Tx
	tracer    *Tracer
	commitErr error
	closed    bool
}

func (f *fakeTx) Begin(ctx context.Context) (pgx.Tx, error) {
	f.query(ctx, "SAVEPOINT sp_1")
	return &fakeTx{tracer: f.tracer, commitErr: f.commitErr}, nil
}
func (f *fakeTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	f.query(ctx, query)
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (f *fakeTx) query(ctx context.Context, sql string) {
	ctx = f.tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: sql})
	f.tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
}
func (f *fakeTx) Commit(ctx context.Context) error {
	f.query(ctx, "COMMIT")
	f.closed = true
	return f.commitErr
}
func (f *fakeTx) Rollback(ctx context.Context) error {
	if f.closed {
		return pgx.ErrTxClosed
	}
	f.query(ctx, "ROLLBACK")
	f.closed = true
	return nil
}

func lifecycleCapture(t *testing.T) (*Tracer, *tracetest.SpanRecorder) {
	t.Helper()
	r := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(r))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(p)
	t.Cleanup(func() { otel.SetTracerProvider(old); p.Shutdown(context.Background()) })
	return New(Config{}), r
}

func TestTransactionSavepointCompletionAndNoDuplicateQuery(t *testing.T) {
	tracer, r := lifecycleCapture(t)
	ctx := context.Background()
	tx, err := tracer.Begin(ctx, &fakeTx{tracer: tracer})
	if err != nil {
		t.Fatal(err)
	}
	nested, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = nested.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	nested, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = nested.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(tx.Rollback(ctx), pgx.ErrTxClosed) {
		t.Fatal("driver return changed")
	}
	want := []string{"BeginTransaction", "Savepoint", "ReleaseSavepoint", "SavepointTransaction", "Savepoint", "RollbackSavepoint", "SavepointTransaction", "CommitTransaction", "Transaction"}
	if len(r.Ended()) != len(want) {
		t.Fatalf("duplicate lifecycle spans: %d", len(r.Ended()))
	}
	for i, span := range r.Ended() {
		if !strings.HasSuffix(span.Name(), want[i]) {
			t.Fatal(span.Name())
		}
		for _, a := range span.Attributes() {
			if strings.HasPrefix(string(a.Key), "db.query.") {
				t.Fatal("lifecycle pretended to analyze SQL")
			}
		}
	}
}

func TestTransactionLifetimeEncompassesQueryAndCleanup(t *testing.T) {
	tracer, r := lifecycleCapture(t)
	ctx := context.Background()
	tx, err := tracer.Begin(ctx, &fakeTx{tracer: tracer})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Ended()) != 1 {
		t.Fatal("transaction lifetime ended at BEGIN")
	}
	if _, err = tx.Exec(ctx, "SELECT 42"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)
	spans := r.Ended()
	if len(spans) != 4 {
		t.Fatalf("spans=%d", len(spans))
	}
	lifetime := spans[3]
	query := spans[1]
	if query.Parent().SpanID() != lifetime.SpanContext().SpanID() || query.StartTime().Before(lifetime.StartTime()) || query.EndTime().After(lifetime.EndTime()) {
		t.Fatal("query outside transaction lifetime")
	}
}

func TestWithinTransactionClosesErrorsPanicsAndBudget(t *testing.T) {
	tracer, r := lifecycleCapture(t)
	failure := errors.New("SECRET callback")
	if err := tracer.WithinTransaction(context.Background(), &fakeTx{tracer: tracer}, func(context.Context, pgx.Tx) error { return failure }); err != failure {
		t.Fatal("callback error changed")
	}
	if r.Ended()[len(r.Ended())-1].Status().Code != codes.Error {
		t.Fatal("callback failure lost")
	}
	func() {
		defer func() {
			if recover() != failure {
				t.Fatal("panic changed")
			}
		}()
		_ = tracer.WithinTransaction(context.Background(), &fakeTx{tracer: tracer}, func(context.Context, pgx.Tx) error { panic(failure) })
	}()
	if !strings.HasSuffix(r.Ended()[len(r.Ended())-1].Name(), "Transaction") {
		t.Fatal("panic leaked lifetime")
	}
	ctx, cancel := outcome.WithWorkBudget(context.Background(), -time.Second)
	defer cancel()
	_ = tracer.WithinTransaction(ctx, &fakeTx{tracer: tracer}, func(context.Context, pgx.Tx) error { return context.DeadlineExceeded })
	last := r.Ended()[len(r.Ended())-1]
	for _, a := range last.Attributes() {
		if a.Key == "operation.outcome" && a.Value.AsString() != "deferred" {
			t.Fatal("cleanup lost budget cause")
		}
	}
}

func TestPoolAcquireCancellationAndDeferredObserver(t *testing.T) {
	tracer, r := lifecycleCapture(t)
	var observed string
	tracer.config.Observe = func(_ context.Context, op, result string, _ time.Duration) {
		if op != "ACQUIRE" && op != "RELEASE" {
			t.Fatal(op)
		}
		observed = result
	}
	ctx, cancel := outcome.WithWorkBudget(context.Background(), -time.Second)
	defer cancel()
	ctx = tracer.TraceAcquireStart(ctx, nil, pgxpool.TraceAcquireStartData{})
	tracer.TraceAcquireEnd(ctx, nil, pgxpool.TraceAcquireEndData{Err: context.DeadlineExceeded})
	if observed != "deferred" || r.Ended()[0].Status().Code == codes.Error {
		t.Fatal(observed)
	}
	tracer.TraceRelease(nil, pgxpool.TraceReleaseData{})
	if len(r.Ended()) != 2 {
		t.Fatal("release missing")
	}
}

func TestTransactionRetryBoundsAndOriginalFailure(t *testing.T) {
	tracer, r := lifecycleCapture(t)
	failure := errors.New("SECRET bind value")
	err := tracer.Retry(context.Background(), RetryConfig{MaxAttempts: 3, Retryable: func(error) bool { return true }}, func(ctx context.Context, attempt int) error { return failure })
	if err != failure || len(r.Ended()) != 3 {
		t.Fatal("retry result/bound")
	}
	for _, span := range r.Ended() {
		if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "SECRET") {
			t.Fatal("error text leaked")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err = tracer.Retry(ctx, RetryConfig{MaxAttempts: 10, Retryable: func(error) bool { return true }, Delay: func(int) time.Duration { return time.Hour }}, func(context.Context, int) error { calls++; cancel(); return failure })
	if err != failure || calls != 1 {
		t.Fatal("cancellation retried or original error replaced")
	}
}
