package traceprocessor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zfogg/gotel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type exporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (e *exporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, spans...)
	return nil
}
func (e *exporter) Shutdown(context.Context) error { return nil }

func TestOperationsRetainErrorsBetweenHealthySamples(t *testing.T) {
	ex := &exporter{}
	processor := New(Config{Application: gotel.NewApplication("test"), Exporter: ex, HealthyEvery: 20})
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	defer provider.Shutdown(context.Background())
	// Use the provider directly so the test does not replace process globals.
	ctx, span := provider.Tracer("test/background").Start(context.WithValue(t.Context(), operationKey{}, &operationState{name: "job"}), "test.job")
	span.SetStatus(2, "failure")
	span.End()
	_ = ctx
	flush, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := processor.ForceFlush(flush); err != nil {
		t.Fatal(err)
	}
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if len(ex.spans) != 1 {
		t.Fatalf("exported spans=%d, want 1", len(ex.spans))
	}
}
