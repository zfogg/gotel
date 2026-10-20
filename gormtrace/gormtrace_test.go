package gormtrace

import (
	"context"
	"testing"

	"github.com/zfogg/gotel/sqltrace"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPluginNamesAndSanitizesEveryOperation(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	namer := sqltrace.Namer{Prefix: "example.storage"}
	if err := db.Use(New(Config{Scope: "example/storage", Namer: namer, System: "sqlite", Namespace: "test", SpanKind: trace.SpanKindInternal})); err != nil {
		t.Fatal(err)
	}
	ctx := namer.WithName(context.Background(), "FindSecret")
	var value string
	if err := db.WithContext(ctx).Raw("SELECT ?", "top-secret").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}

	for _, span := range recorder.Ended() {
		if span.Name() != "example.storage.FindSecret" {
			continue
		}
		attrs := map[string]any{}
		for _, attr := range span.Attributes() {
			switch string(attr.Key) {
			case "db.query.text", "db.query.summary":
				attrs[string(attr.Key)] = attr.Value.AsString()
			case "db.query.parameter.count":
				attrs[string(attr.Key)] = attr.Value.AsInt64()
			}
		}
		if attrs["db.query.text"] != "SELECT ?" {
			t.Fatalf("query text = %#v", attrs["db.query.text"])
		}
		if attrs["db.query.summary"] != "example.storage.FindSecret" {
			t.Fatalf("query summary = %#v", attrs["db.query.summary"])
		}
		if attrs["db.query.parameter.count"] != int64(1) {
			t.Fatalf("parameter count = %#v", attrs["db.query.parameter.count"])
		}
		if span.SpanKind() != trace.SpanKindInternal {
			t.Fatalf("span kind = %v", span.SpanKind())
		}
		return
	}
	t.Fatal("named database span not recorded")
}
