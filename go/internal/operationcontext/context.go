// Package operationcontext identifies the explicit operation owning a context.
package operationcontext

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

type key struct{}

// WithSpan marks an operation without changing its span, values or cancellation.
func WithSpan(ctx context.Context, span trace.Span) context.Context {
	return context.WithValue(ctx, key{}, span.SpanContext())
}

// IsCurrent distinguishes an explicit operation from an inherited request or
// transaction context. Descendant spans retain the marker but are not the
// marked operation themselves.
func IsCurrent(ctx context.Context) bool {
	operation, ok := ctx.Value(key{}).(trace.SpanContext)
	current := trace.SpanContextFromContext(ctx)
	return ok && current.IsValid() && operation.TraceID() == current.TraceID() && operation.SpanID() == current.SpanID()
}
