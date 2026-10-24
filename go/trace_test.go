package gotel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestScopeTracerQualifiesSpanNamesOnce(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	}()

	tracer := (Scope{Prefix: "ratio"}).Tracer("auth")
	for _, name := range []string{"auth.verify", "ratio.http.server.GET /orders"} {
		_, span := tracer.Start(t.Context(), name)
		span.End()
	}

	spans := recorder.Ended()
	if got := spans[0].Name(); got != "ratio.auth.verify" {
		t.Fatalf("unqualified span name = %q", got)
	}
	if got := spans[1].Name(); got != "ratio.http.server.GET /orders" {
		t.Fatalf("already-qualified span name = %q", got)
	}
}
