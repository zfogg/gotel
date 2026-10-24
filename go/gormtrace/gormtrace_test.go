package gormtrace

import (
	"context"
	"testing"

	"github.com/zfogg/gotel/go/sqltrace"
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
			case "gotel.db.query.bind_count":
				attrs[string(attr.Key)] = attr.Value.AsInt64()
			}
		}
		if attrs["db.query.text"] != "SELECT\n  ?" {
			t.Fatalf("query text = %#v", attrs["db.query.text"])
		}
		if attrs["db.query.summary"] != "SELECT …" {
			t.Fatalf("query summary = %#v", attrs["db.query.summary"])
		}
		if attrs["gotel.db.query.bind_count"] != int64(1) {
			t.Fatalf("parameter count = %#v", attrs["gotel.db.query.bind_count"])
		}
		if span.SpanKind() != trace.SpanKindInternal {
			t.Fatalf("span kind = %v", span.SpanKind())
		}
		return
	}
	t.Fatal("named database span not recorded")
}

func TestPluginCoversCRUDAndSuppression(t *testing.T) {
	type widget struct {
		ID   uint
		Name string
	}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	namer := sqltrace.MustNamer("example.storage")
	if err := db.Use(New(Config{Namer: namer, System: "sqlite", Dialect: sqltrace.DialectSQLite})); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&widget{}); err != nil {
		t.Fatal(err)
	}
	operations := []struct {
		name string
		run  func(*gorm.DB) error
	}{
		{"CreateWidget", func(tx *gorm.DB) error { return tx.Create(&widget{Name: "secret"}).Error }},
		{"FindWidget", func(tx *gorm.DB) error { var got widget; return tx.First(&got, 1).Error }},
		{"UpdateWidget", func(tx *gorm.DB) error { return tx.Model(&widget{}).Where("id = ?", 1).Update("name", "updated").Error }},
		{"DeleteWidget", func(tx *gorm.DB) error { return tx.Delete(&widget{}, 1).Error }},
		{"CountWidgets", func(tx *gorm.DB) error { var count int64; return tx.Model(&widget{}).Count(&count).Error }},
	}
	for _, operation := range operations {
		if err := operation.run(db.WithContext(namer.WithName(t.Context(), operation.name))); err != nil {
			t.Fatalf("%s: %v", operation.name, err)
		}
	}
	before := len(recorder.Ended())
	var ignored []widget
	if err := db.WithContext(sqltrace.WithoutTracing(t.Context())).Find(&ignored).Error; err != nil {
		t.Fatal(err)
	}
	if got := len(recorder.Ended()); got != before {
		t.Fatalf("suppressed operation recorded %d spans", got-before)
	}
	seen := make(map[string]bool)
	for _, span := range recorder.Ended() {
		seen[span.Name()] = true
	}
	for _, operation := range operations {
		if !seen[namer.Name(operation.name)] {
			t.Errorf("missing %s span", operation.name)
		}
	}
}
