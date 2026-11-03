// Package outcome classifies completion without changing errors returned to callers.
package outcome

import (
	"context"
	"errors"
	"time"

	"github.com/zfogg/gotel/go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Value is a bounded telemetry outcome; it never replaces a returned error.
type Value string

const (
	Success   Value = "success"   // Completed successfully.
	Error     Value = "error"     // Failed; error messages are excluded from telemetry.
	Cancelled Value = "cancelled" // Caller cancelled work without a failure outcome.
	Deferred  Value = "deferred"  // Owner explicitly preserves work for a later attempt.
)

var errWorkBudget = errors.New("gotel: resumable work budget exhausted")

// WithWorkBudget marks only this deadline as resumable. The owner must preserve
// unfinished work. An earlier caller cancellation or deadline remains a failure.
func WithWorkBudget(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(ctx, budget, errWorkBudget)
}

// WorkBudgetExpired recognizes only a private WithWorkBudget deadline cause
// paired with a cancellation/deadline error, including wrapped errors.
func WorkBudgetExpired(ctx context.Context, err error) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errWorkBudget) &&
		(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

// Classify never treats an arbitrary error occurring after cancellation as deferred.
func Classify(ctx context.Context, err error) Value {
	if err == nil {
		return Success
	}
	if WorkBudgetExpired(ctx, err) {
		return Deferred
	}
	if errors.Is(err, context.Canceled) {
		return Cancelled
	}
	return Error
}

// Apply records safe completion attributes without ending the span or retaining
// exception messages, SQL, or parameter values. Deferred/cancelled are not errors.
func Apply(span trace.Span, value Value, err error) {
	if span == nil {
		return
	}
	switch value {
	case Success, Error, Cancelled, Deferred:
	default:
		value = Error
	}
	span.SetAttributes(attribute.String("operation.outcome", string(value)))
	if value == Deferred {
		span.AddEvent("work_budget_exhausted")
	}
	if value == Error {
		span.SetStatus(codes.Error, "")
		if err != nil {
			span.SetAttributes(attribute.String("error.type", gotel.ErrorType(err)))
		}
	}
}

// End applies the default classification and ends span, preserving err unchanged.
func End(ctx context.Context, span trace.Span, err error) {
	if span != nil {
		Apply(span, Classify(ctx, err), err)
		span.End()
	}
}
