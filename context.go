package gotel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// BackgroundContext keeps trace lineage while taking cancellation and deadline
// exclusively from owner. Request values and cancellation never leak into work
// that has already been admitted by the application.
func BackgroundContext(owner, trigger context.Context) context.Context {
	return trace.ContextWithSpanContext(owner, trace.SpanContextFromContext(trigger))
}

func (a Application) StartBackground(owner, trigger context.Context, component, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return a.Start(BackgroundContext(owner, trigger), component, name, attrs...)
}

// CleanupContext preserves correlation and values after cancellation and gives
// cleanup its own bounded lifetime.
func CleanupContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}
