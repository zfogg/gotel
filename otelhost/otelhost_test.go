package otelhost

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestStartWithInjectedTraceExporter(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	runtime, err := Start(context.Background(), Config{
		ServiceName:    "example",
		ServiceVersion: "1.2.3",
		Environment:    "test",
		Signals:        Traces,
		TraceExporter:  exporter,
		Attributes:     []attribute.KeyValue{attribute.String("tenant", "demo")},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("test").Start(context.Background(), "probe")
	span.End()
	if err := runtime.TracerProvider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "probe" {
		t.Fatalf("exported spans = %#v", spans)
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStartRequiresServiceName(t *testing.T) {
	if _, err := Start(context.Background(), Config{}); err == nil {
		t.Fatal("missing service name accepted")
	}
}
