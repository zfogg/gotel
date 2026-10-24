package otelhost

import (
	"context"
	"os"
	goruntime "runtime"
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
	attrs := spans[0].Resource.Set()
	for _, key := range []attribute.Key{"host.name", "os.type", "os.description", "process.executable.name", "process.executable.path", "process.runtime.description", "telemetry.sdk.version"} {
		if value, ok := attrs.Value(key); !ok || value.AsString() == "" {
			t.Errorf("missing resource attribute %s", key)
		}
	}
	for key, want := range map[attribute.Key]string{
		"service.name": "example", "service.version": "1.2.3", "deployment.environment.name": "test",
		"process.runtime.version": goruntime.Version(), "telemetry.sdk.language": "go", "telemetry.sdk.name": "opentelemetry", "tenant": "demo",
	} {
		if value, _ := attrs.Value(key); value.AsString() != want {
			t.Errorf("%s = %v, want %q", key, value, want)
		}
	}
	if value, _ := attrs.Value("process.pid"); value.AsInt64() != int64(os.Getpid()) {
		t.Errorf("process.pid = %v", value)
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

func TestSpanNamePrefixOption(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		exporter := tracetest.NewInMemoryExporter()
		runtime, err := Start(context.Background(), Config{ServiceName: "demo-api", ApplicationName: "demo", DisableSpanNamePrefix: disabled, Signals: Traces, TraceExporter: exporter})
		if err != nil {
			t.Fatal(err)
		}
		_, span := runtime.TracerProvider.Tracer("demo/http").Start(context.Background(), "demo.http.server.GET /items")
		span.SetName("demo.http.server.POST /items")
		span.End()
		_ = runtime.TracerProvider.ForceFlush(context.Background())
		want := "demo.http.server.POST /items"
		if disabled {
			want = "http.server.POST /items"
		}
		if exporter.GetSpans()[0].Name != want {
			t.Fatal(exporter.GetSpans()[0].Name)
		}
		_ = runtime.Shutdown(context.Background())
	}
}
