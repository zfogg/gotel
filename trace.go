// Package gotel provides small, reusable OpenTelemetry building blocks for Go
// applications. It deliberately leaves exporter policy to the application.
package gotel

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Scope gives every component in an application a consistent instrumentation
// scope without repeating string concatenation throughout the codebase.
type Scope struct {
	// Prefix is the application or library name prepended to component scopes.
	Prefix string
}

type scopedTracer struct {
	trace.Tracer
	prefix string
}

func (t scopedTracer) Start(ctx context.Context, name string, options ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.Tracer.Start(ctx, qualify(t.prefix, name), options...)
}

// Tracer returns a tracer named <prefix>/<component>. Empty parts are omitted.
func (s Scope) Tracer(component string) trace.Tracer {
	prefix := strings.TrimSpace(s.Prefix)
	name := strings.Trim(prefix+"/"+strings.TrimSpace(component), "/")
	return scopedTracer{Tracer: otel.Tracer(name), prefix: prefix}
}

// Start starts an internal span in this scope.
func (s Scope) Start(ctx context.Context, component, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return s.Tracer(component).Start(ctx, name, trace.WithAttributes(attrs...))
}

func qualify(prefix, name string) string {
	prefix = strings.TrimSuffix(strings.TrimSpace(prefix), ".")
	name = strings.TrimSpace(name)
	if prefix == "" || name == prefix || strings.HasPrefix(name, prefix+".") {
		return name
	}
	return prefix + "." + name
}

// Correlation returns trace and span IDs that are safe to persist with an
// application artifact. Empty values mean ctx has no valid span context.
func Correlation(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}

// Finish records duration and an optional error before ending span.
func Finish(span trace.Span, started time.Time, err error) {
	if span == nil {
		return
	}
	span.SetAttributes(attribute.Float64("operation.duration_seconds", time.Since(started).Seconds()))
	if err != nil {
		RecordError(span, err)
	}
	span.End()
}

// End records an optional error and ends span.
func End(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		RecordError(span, err)
	}
	span.End()
}
