package outcome

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestBudgetCauseAndOriginalError(t *testing.T) {
	ctx, cancel := WithWorkBudget(context.Background(), -time.Second)
	defer cancel()
	err := fmt.Errorf("driver: %w", context.DeadlineExceeded)
	if Classify(ctx, err) != Deferred || !WorkBudgetExpired(ctx, err) {
		t.Fatal("budget not deferred")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("original error changed")
	}
	if Classify(ctx, errors.New("private value")) != Error {
		t.Fatal("unrelated error deferred")
	}
	if Classify(ctx, nil) != Success {
		t.Fatal("successful completion classified as deferred")
	}
	parent, stop := context.WithCancel(context.Background())
	stop()
	child, finish := WithWorkBudget(parent, -time.Second)
	defer finish()
	if Classify(child, context.Canceled) != Cancelled {
		t.Fatal("caller cancellation overridden")
	}
	deadline, end := context.WithTimeout(context.Background(), -time.Second)
	defer end()
	child, finish = WithWorkBudget(deadline, time.Hour)
	defer finish()
	if Classify(child, context.DeadlineExceeded) != Error {
		t.Fatal("caller deadline deferred")
	}
}

func TestOutcomesStatusAndPrivacy(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())
	for _, value := range []Value{Success, Error, Cancelled, Deferred} {
		_, span := provider.Tracer("test").Start(context.Background(), string(value))
		Apply(span, value, errors.New("SECRET SQL value"))
		span.End()
	}
	for _, span := range recorder.Ended() {
		if (span.Status().Code == codes.Error) != (span.Name() == string(Error)) {
			t.Fatalf("status %s: %v", span.Name(), span.Status())
		}
		if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "SECRET") {
			t.Fatal("error text leaked")
		}
		if span.Name() == string(Deferred) && (len(span.Events()) != 1 || span.Events()[0].Name != "work_budget_exhausted") {
			t.Fatal("missing deferred event")
		}
	}
}
