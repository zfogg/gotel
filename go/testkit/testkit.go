// Package testkit provides deterministic in-memory OpenTelemetry assertions.
package testkit

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
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

func RequireBoundedNames(t testing.TB, spans []sdktrace.ReadOnlySpan) {
	t.Helper()
	for _, span := range spans {
		if err := ValidateSpanName(span.Name()); err != nil {
			t.Error(err)
		}
	}
}

// RequireNoDuplicateSpans rejects identically timed, named, and parented spans.
func RequireNoDuplicateSpans(t testing.TB, spans []sdktrace.ReadOnlySpan) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, span := range spans {
		// Different IDs can still be duplicated instrumentation, so omit the ID.
		key := fmt.Sprintf("%s|%s|%s|%d|%d", span.SpanContext().TraceID(), span.Name(), span.Parent().SpanID(), span.StartTime().UnixNano(), span.EndTime().UnixNano())
		if _, ok := seen[key]; ok {
			t.Errorf("duplicate span: %s", span.Name())
		}
		seen[key] = struct{}{}
	}
}

// RequireTemporalTopology verifies one workflow root and its activity children.
func RequireTemporalTopology(t testing.TB, spans []sdktrace.ReadOnlySpan, prefix, workflow string, activities ...string) {
	t.Helper()
	RequireNoDuplicateSpans(t, spans)
	wantWorkflow := prefix + ".temporal.workflow." + workflow
	var root trace.SpanID
	for _, span := range spans {
		if span.Name() == wantWorkflow {
			if root.IsValid() {
				t.Errorf("multiple workflow spans %q", wantWorkflow)
			}
			root = span.SpanContext().SpanID()
		}
	}
	if !root.IsValid() {
		t.Errorf("missing workflow span %q", wantWorkflow)
		return
	}
	for _, activity := range activities {
		want := prefix + ".temporal.activity." + activity
		count := 0
		for _, span := range spans {
			if span.Name() == want {
				count++
				if span.Parent().SpanID() != root {
					t.Errorf("activity %q is not child of workflow", want)
				}
			}
		}
		if count != 1 {
			t.Errorf("activity span %q count=%d, want 1", want, count)
		}
	}
}

// RequireSpanName reports whether a smoke operation installed instrumentation.
func RequireSpanName(t testing.TB, spans []sdktrace.ReadOnlySpan, name string) {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return
		}
	}
	t.Errorf("missing span %q", name)
}
