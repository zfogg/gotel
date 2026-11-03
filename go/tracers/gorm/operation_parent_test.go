package gorm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/zfogg/gotel/go"
	"github.com/zfogg/gotel/go/outcome"
	"github.com/zfogg/gotel/go/tracers/processor"
	gotelsql "github.com/zfogg/gotel/go/tracers/sql"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type driverObservation struct {
	method string
	query  string
	span   trace.SpanContext
}

// Observe the context that actually reaches the native driver, after both the
// GORM callback and the transaction wrapper have had an opportunity to bind it.
type observingConnector struct {
	driver  *sqlite3.SQLiteDriver
	observe func(context.Context, string, string)
}

func (c observingConnector) Driver() driver.Driver { return c.driver }
func (c observingConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.driver.Open(":memory:")
	if err != nil {
		return nil, err
	}
	return observingConnection{SQLiteConn: conn.(*sqlite3.SQLiteConn), observe: c.observe}, nil
}

type observingConnection struct {
	*sqlite3.SQLiteConn
	observe func(context.Context, string, string)
}

func (c observingConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.observe(ctx, "exec", query)
	return c.SQLiteConn.ExecContext(ctx, query, args)
}
func (c observingConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.observe(ctx, "query", query)
	return c.SQLiteConn.QueryContext(ctx, query, args)
}
func (c observingConnection) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.observe(ctx, "prepare", query)
	stmt, err := c.SQLiteConn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return observingStatement{Stmt: stmt, query: query, observe: c.observe}, nil
}

type observingStatement struct {
	driver.Stmt
	query   string
	observe func(context.Context, string, string)
}

func (s observingStatement) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.observe(ctx, "exec", s.query)
	return s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
}
func (s observingStatement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.observe(ctx, "query", s.query)
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
}

func operationParentFixture(t *testing.T, prepared bool) (*gorm.DB, *tracetest.SpanRecorder, *[]driverObservation) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	observations := []driverObservation{}
	raw := sql.OpenDB(observingConnector{driver: &sqlite3.SQLiteDriver{}, observe: func(ctx context.Context, method, query string) {
		observations = append(observations, driverObservation{method, query, trace.SpanContextFromContext(ctx)})
	}})
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db, err := gorm.Open(sqlite.New(sqlite.Config{Conn: WrapTransactions(raw, gotel.NewApplication(""), "sqlite")}), &gorm.Config{PrepareStmt: prepared})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Use(New(Config{System: "sqlite"})); err != nil {
		t.Fatal(err)
	}
	return db, recorder, &observations
}

func requireNamedSpan(t *testing.T, recorder *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var found sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			if found != nil {
				t.Fatalf("duplicate %s span", name)
			}
			found = span
		}
	}
	if found == nil {
		t.Fatalf("missing %s span", name)
	}
	return found
}

func TestTransactionQueriesPreserveExplicitOperationParent(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprintf("prepared_%t", prepared), func(t *testing.T) {
			db, recorder, observations := operationParentFixture(t, prepared)
			ctx, request := otel.Tracer("test").Start(t.Context(), "request")
			defer request.End()
			tx := db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			operationCtx, operation := otel.Tracer("test").Start(ctx, "business.admission")
			namer := gotelsql.StorageNamer("sqlite")
			var answer int
			if err := tx.WithContext(namer.WithName(operationCtx, "GetAccount")).Raw("SELECT ?", 42).Scan(&answer).Error; err != nil || answer != 42 {
				t.Fatal(err, answer)
			}
			operation.End()
			// Reusing the original transaction caller context must still bind an
			// ordinary query to the transaction, rather than to the request.
			if err := tx.WithContext(namer.WithName(ctx, "PlainAccount")).Raw("SELECT ?", 7).Scan(&answer).Error; err != nil || answer != 7 {
				t.Fatal(err, answer)
			}
			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
			lifetime := requireNamedSpan(t, recorder, "storage.sqlite.Transaction")
			query := requireNamedSpan(t, recorder, "storage.sqlite.GetAccount")
			plain := requireNamedSpan(t, recorder, "storage.sqlite.PlainAccount")
			commit := requireNamedSpan(t, recorder, "storage.sqlite.CommitTransaction")
			if query.Parent().SpanID() != operation.SpanContext().SpanID() {
				t.Fatalf("business query parent=%s, want operation %s (transaction %s)", query.Parent().SpanID(), operation.SpanContext().SpanID(), lifetime.SpanContext().SpanID())
			}
			if plain.Parent().SpanID() != lifetime.SpanContext().SpanID() || commit.Parent().SpanID() != lifetime.SpanContext().SpanID() || lifetime.Parent().SpanID() != request.SpanContext().SpanID() {
				t.Fatal("plain query or transaction lifecycle parent changed")
			}
			nativeQueries := []driverObservation{}
			for _, observation := range *observations {
				if observation.query == "SELECT ?" && observation.method == "query" {
					nativeQueries = append(nativeQueries, observation)
				}
			}
			if len(nativeQueries) != 2 || nativeQueries[0].span.SpanID() != query.SpanContext().SpanID() || nativeQueries[1].span.SpanID() != plain.SpanContext().SpanID() {
				t.Fatalf("native driver lost the GORM query span: %+v", nativeQueries)
			}
		})
	}
}

func TestTransactionOperationParentPreservesSavepointsAndRollback(t *testing.T) {
	db, recorder, _ := operationParentFixture(t, true)
	ctx, request := otel.Tracer("test").Start(t.Context(), "request")
	defer request.End()
	wantErr := errors.New("business rejected")
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.SavePoint("before_operation").Error; err != nil {
			return err
		}
		operationCtx, operation := otel.Tracer("test").Start(ctx, "business.rollback")
		defer operation.End()
		var answer int
		if err := tx.WithContext(gotelsql.StorageNamer("sqlite").WithName(operationCtx, "BeforeRollback")).Raw("SELECT ?", 42).Scan(&answer).Error; err != nil {
			return err
		}
		if err := tx.RollbackTo("before_operation").Error; err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("transaction error changed: %v", err)
	}
	lifetime := requireNamedSpan(t, recorder, "storage.sqlite.Transaction")
	rollback := requireNamedSpan(t, recorder, "storage.sqlite.RollbackTransaction")
	operation := requireNamedSpan(t, recorder, "business.rollback")
	query := requireNamedSpan(t, recorder, "storage.sqlite.BeforeRollback")
	if query.Parent().SpanID() != operation.SpanContext().SpanID() || rollback.Parent().SpanID() != lifetime.SpanContext().SpanID() {
		t.Fatal("operation or rollback parent changed")
	}
	if lifetime.EndTime().Before(query.EndTime()) || lifetime.EndTime().Before(rollback.EndTime()) {
		t.Fatal("transaction lifetime ended before its work")
	}
}

func TestTransactionPreservesOperationStartedBeforeBegin(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		for _, background := range []bool{false, true} {
			t.Run(fmt.Sprintf("prepared_%t/background_%t", prepared, background), func(t *testing.T) {
				db, recorder, observations := operationParentFixture(t, prepared)
				ctx := t.Context()
				if !background {
					var request trace.Span
					ctx, request = otel.Tracer("test").Start(ctx, "request")
					defer request.End()
				}
				ctx, finish := processor.NewOperations(gotel.NewApplication("")).Start(ctx, "business.admission")
				operationID := trace.SpanContextFromContext(ctx).SpanID()
				err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					var answer int
					return tx.WithContext(gotelsql.StorageNamer("sqlite").WithName(ctx, "GetAccount")).Raw("SELECT ?", 42).Scan(&answer).Error
				})
				finish(err)
				if err != nil {
					t.Fatal(err)
				}
				query := requireNamedSpan(t, recorder, "storage.sqlite.GetAccount")
				lifetime := requireNamedSpan(t, recorder, "storage.sqlite.Transaction")
				commit := requireNamedSpan(t, recorder, "storage.sqlite.CommitTransaction")
				if query.Parent().SpanID() != operationID || lifetime.Parent().SpanID() != operationID || commit.Parent().SpanID() != lifetime.SpanContext().SpanID() {
					t.Fatal("pre-existing operation or transaction lifecycle parent changed")
				}
				for _, observation := range *observations {
					if observation.query == "SELECT ?" && observation.span.SpanID() != query.SpanContext().SpanID() {
						t.Fatalf("native %s overwrote the query span", observation.method)
					}
				}
			})
		}
	}
}

func TestTransactionDriverMethodsKeepCallerContextAndCancellation(t *testing.T) {
	db, recorder, observations := operationParentFixture(t, false)
	ctx, request := otel.Tracer("test").Start(t.Context(), "request")
	defer request.End()
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	pool := tx.Statement.ConnPool.(*tracedTransaction)
	operationCtx, operation := otel.Tracer("test").Start(ctx, "business.direct_sql")
	defer operation.End()
	if _, err := pool.ExecContext(ctx, "CREATE TABLE context_probe(id integer)"); err != nil {
		t.Fatal(err)
	}
	stmt, err := pool.PrepareContext(operationCtx, "INSERT INTO context_probe VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if _, err = stmt.ExecContext(operationCtx, 42); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.QueryContext(operationCtx, "SELECT id FROM context_probe")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("missing inserted row")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(operationCtx)
	cancel()
	if _, err := pool.ExecContext(cancelled, "SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation changed: %v", err)
	}
	budget, stop := outcome.WithWorkBudget(operationCtx, -time.Second)
	defer stop()
	if _, err := pool.ExecContext(budget, "SELECT 1"); !errors.Is(err, context.DeadlineExceeded) || outcome.Classify(pool.traceContext(budget), err) != outcome.Deferred {
		t.Fatalf("work budget cause changed: %v", err)
	}
	var answer int
	if err := pool.QueryRowContext(ctx, "SELECT id FROM context_probe").Scan(&answer); err != nil || answer != 42 {
		t.Fatal("failed query changed transaction usability", err, answer)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, "SELECT 1"); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("completed transaction result changed: %v", err)
	}
	lifetime := requireNamedSpan(t, recorder, "storage.sqlite.Transaction")
	queries := 0
	for _, observation := range *observations {
		want := lifetime.SpanContext().SpanID()
		if observation.query == "INSERT INTO context_probe VALUES (?)" {
			want = operation.SpanContext().SpanID()
		}
		if observation.query == "SELECT id FROM context_probe" && observation.method == "query" {
			if queries == 0 {
				want = operation.SpanContext().SpanID()
			}
			queries++
		}
		if observation.query == "CREATE TABLE context_probe(id integer)" || observation.query == "INSERT INTO context_probe VALUES (?)" || observation.query == "SELECT id FROM context_probe" {
			if observation.span.SpanID() != want {
				t.Fatalf("native %s parent changed for %s", observation.method, observation.query)
			}
		}
	}
	if queries != 2 {
		t.Fatalf("native query count=%d, want explicit query followed by plain row query", queries)
	}
}
