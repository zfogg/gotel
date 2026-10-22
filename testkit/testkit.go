// Package testkit provides deterministic in-memory OpenTelemetry assertions.
package testkit

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func Install(t testing.TB) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	return recorder
}

var uuid = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

// ValidateSpanName catches common accidental cardinality sources in tests and
// development without constraining intentional external host names or routes.
func ValidateSpanName(name string) error {
	if strings.ContainsAny(name, "?#") {
		return &CardinalityError{Name: name, Reason: "query or fragment"}
	}
	if uuid.MatchString(name) {
		return &CardinalityError{Name: name, Reason: "UUID"}
	}
	return nil
}

type CardinalityError struct{ Name, Reason string }

func (e *CardinalityError) Error() string {
	return "high-cardinality span name (" + e.Reason + "): " + e.Name
}

func RequireBoundedNames(t testing.TB, spans tracetest.SpanStubs) {
	t.Helper()
	for _, span := range spans {
		if err := ValidateSpanName(span.Name); err != nil {
			t.Error(err)
		}
	}
}
