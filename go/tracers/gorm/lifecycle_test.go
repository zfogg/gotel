package gorm

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/zfogg/gotel/go"
	"github.com/zfogg/gotel/go/outcome"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func transactionFixture(t *testing.T) (*gorm.DB, *sql.DB, *tracetest.SpanRecorder) {
	t.Helper()
	r := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(r))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(p)
	t.Cleanup(func() { otel.SetTracerProvider(old); _ = p.Shutdown(context.Background()) })
	raw, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db, err := gorm.Open(sqlite.New(sqlite.Config{Conn: WrapTransactions(raw, gotel.NewApplication(""), "sqlite")}), &gorm.Config{PrepareStmt: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Use(New(Config{System: "sqlite"})); err != nil {
		t.Fatal(err)
	}
	return db, raw, r
}

func TestGORMTransactionLifetimePreparedQueryAndCleanup(t *testing.T) {
	db, _, r := transactionFixture(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	var answer int
	if err := tx.Raw("SELECT ?", 42).Scan(&answer).Error; err != nil || answer != 42 {
		t.Fatal(err, answer)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(tx.Rollback().Error, sql.ErrTxDone) {
		t.Fatal("cleanup return changed")
	}
	var lifetime, query sdktrace.ReadOnlySpan
	counts := map[string]int{}
	for _, span := range r.Ended() {
		counts[span.Name()]++
		if span.Name() == "storage.sqlite.Transaction" {
			lifetime = span
		}
		for _, a := range span.Attributes() {
			if a.Key == "db.query.text" {
				query = span
			}
		}
	}
	if counts["storage.sqlite.Transaction"] != 1 || counts["storage.sqlite.CommitTransaction"] != 1 || counts["storage.sqlite.RollbackTransaction"] != 0 {
		t.Fatal(counts)
	}
	if lifetime == nil || query == nil || query.Parent().SpanID() != lifetime.SpanContext().SpanID() {
		t.Fatal("prepared query not parented to lifetime")
	}
	if query.StartTime().Before(lifetime.StartTime()) || query.EndTime().After(lifetime.EndTime()) {
		t.Fatal("query outside lifetime")
	}
}

func TestGORMCancelledAcquireAndMarkedDeferredLifetime(t *testing.T) {
	db, raw, r := transactionFixture(t)
	conn, err := raw.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = db.WithContext(ctx).Begin().Error; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, span := range r.Ended() {
		if span.Status().Code == codes.Error {
			t.Fatal("cancelled acquire marked failed")
		}
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = outcome.WithWorkBudget(context.Background(), time.Hour)
	defer cancel()
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	budget, stop := outcome.WithWorkBudget(ctx, -time.Second)
	defer stop()
	if !MarkTransactionOutcome(tx.Statement.ConnPool, budget, context.DeadlineExceeded) {
		t.Fatal("could not mark prepared tx")
	}
	if err = tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	last := r.Ended()[len(r.Ended())-1]
	if last.Status().Code == codes.Error {
		t.Fatal("deferred tx failed")
	}
	found := false
	for _, a := range last.Attributes() {
		if a.Key == "operation.outcome" && a.Value.AsString() == "deferred" {
			found = true
		}
	}
	if !found {
		t.Fatal("budget cause lost")
	}
}
