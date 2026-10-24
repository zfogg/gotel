package gotel

import (
	"context"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"testing"
)

func TestWithoutApplicationPrefix(t *testing.T) {
	output := tracetest.NewInMemoryExporter()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSyncer(WithoutApplicationPrefix(output, "demo")))
	ctx, parent := provider.Tracer("demo/storage").Start(context.Background(), "demo.http.server.GET /items")
	_, child := provider.Tracer("demo/storage").Start(ctx, "demo.storage.Find")
	child.End()
	parent.End()
	_, boundary := provider.Tracer("test").Start(ctx, "demolition.Query")
	boundary.End()
	spans := output.GetSpans()
	if spans[0].Name != "storage.Find" || spans[1].Name != "http.server.GET /items" || spans[2].Name != "demolition.Query" {
		t.Fatalf("names: %#v", spans)
	}
	if spans[0].Parent.SpanID() != spans[1].SpanContext.SpanID() {
		t.Fatal("parent changed")
	}
	if recorder.Ended()[0].Name() != "demo.storage.Find" {
		t.Fatal("input span mutated")
	}
	if spans[0].InstrumentationScope.Name != "demo/storage" {
		t.Fatal("scope changed")
	}
	_ = provider.Shutdown(context.Background())
}
