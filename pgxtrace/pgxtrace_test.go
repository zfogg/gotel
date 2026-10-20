package pgxtrace

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/zfogg/gotel/sqltrace"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestQueryTracerUsesSQLCNameAndSafeAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	tracer := New(Config{Scope: "example/storage", Namer: sqltrace.Namer{Prefix: "example.storage"}, Namespace: "example"})
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "-- name: FindWidget :one\nSELECT * FROM widgets WHERE token = $1 AND attempts > 3",
		Args: []any{"top-secret"},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "example.storage.FindWidget" {
		t.Fatalf("spans = %#v", spans)
	}
	attrs := map[string]any{}
	for _, attr := range spans[0].Attributes() {
		switch string(attr.Key) {
		case "db.query.text", "db.query.summary":
			attrs[string(attr.Key)] = attr.Value.AsString()
		case "gotel.db.query.bind_count":
			attrs[string(attr.Key)] = attr.Value.AsInt64()
		}
	}
	if attrs["db.query.text"] != "SELECT * FROM widgets WHERE token = ? AND attempts > ?" {
		t.Fatalf("query text = %#v", attrs["db.query.text"])
	}
	if attrs["gotel.db.query.bind_count"] != int64(1) {
		t.Fatalf("parameter count = %#v", attrs["gotel.db.query.bind_count"])
	}
	if attrs["db.query.summary"] != "SELECT … FROM widgets WHERE …" {
		t.Fatalf("summary = %#v", attrs["db.query.summary"])
	}
}

func TestHookSummariesAreIndependentOfNames(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	namer := sqltrace.MustNamer("example.storage")
	tracer := New(Config{Namer: namer})
	ctx := tracer.TraceQueryStart(namer.WithName(context.Background(), "CustomName"), nil, pgx.TraceQueryStartData{SQL: "SELECT * FROM accounts"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	ctx = tracer.TraceCopyFromStart(context.Background(), nil, pgx.TraceCopyFromStartData{TableName: pgx.Identifier{"public", "bars"}})
	tracer.TraceCopyFromEnd(ctx, nil, pgx.TraceCopyFromEndData{})
	ctx = tracer.TracePrepareStart(context.Background(), nil, pgx.TracePrepareStartData{SQL: "SELECT * FROM bars", Name: "prepared"})
	tracer.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})
	for i, want := range []string{"SELECT … FROM accounts", "COPY \"public\".\"bars\"", "SELECT … FROM bars"} {
		got := ""
		for _, attr := range recorder.Ended()[i].Attributes() {
			if attr.Key == sqltrace.QuerySummaryKey {
				got = attr.Value.AsString()
			}
		}
		if got != want {
			t.Errorf("hook %d: %q, want %q", i, got, want)
		}
	}
	if recorder.Ended()[0].Name() != "example.storage.CustomName" {
		t.Fatal("context name changed")
	}
}
