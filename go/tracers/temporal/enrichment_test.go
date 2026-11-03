package temporal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zfogg/gotel/go"
	"github.com/zfogg/gotel/go/outcome"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func TestSDKActivityEnrichmentLinksAndErrorPrivacy(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() { otel.SetTracerProvider(previous); provider.Shutdown(context.Background()) }()
	prior := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	tracing, err := New(Config{Application: gotel.NewApplication("fixture"), ExecutionLinks: func(context.Context) []trace.Link { return []trace.Link{{SpanContext: prior}} }})
	if err != nil {
		t.Fatal(err)
	}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.SetWorkerOptions(worker.Options{Interceptors: []interceptor.WorkerInterceptor{tracing}})
	activityFn := func(context.Context) (string, error) { return "", errors.New("PRIVATE activity payload") }
	env.RegisterActivity(activityFn)
	_, err = env.ExecuteActivity(activityFn)
	if err == nil {
		t.Fatal("driver error swallowed")
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("SDK duplicate spans: %d", len(spans))
	}
	span := spans[0]
	if !strings.HasPrefix(span.Name(), "fixture.temporal.activity.execute.") {
		t.Fatal(span.Name())
	}
	if span.Status().Code != codes.Error || len(span.Links()) != 1 || span.Links()[0].SpanContext.TraceID() != prior.TraceID() {
		t.Fatal("status/links")
	}
	found := false
	for _, attr := range span.Attributes() {
		if attr.Key == "temporal.activity.attempt" && attr.Value.AsInt64() == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("missing actual SDK attempt")
	}
	if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "PRIVATE") {
		t.Fatal("activity error text leaked")
	}
}

func TestWorkflowMetadataOnExistingSDKSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(p)
	defer func() { otel.SetTracerProvider(old); p.Shutdown(context.Background()) }()
	tracing, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{Interceptors: []interceptor.WorkerInterceptor{tracing}})
	workflowFn := func(ctx workflow.Context) (string, error) { return "done", nil }
	env.RegisterWorkflow(workflowFn)
	env.ExecuteWorkflow(workflowFn)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, span := range recorder.Ended() {
		if strings.HasPrefix(span.Name(), "temporal.workflow.") {
			found = true
			attempt := false
			for _, a := range span.Attributes() {
				if a.Key == "temporal.workflow.attempt" && a.Value.AsInt64() == 1 {
					attempt = true
				}
			}
			if !attempt {
				t.Fatal("missing workflow attempt")
			}
		}
	}
	if !found {
		t.Fatal("missing SDK workflow span")
	}
}

func TestActivityRetryDelayAndBenignCancellation(t *testing.T) {
	now := time.Now()
	attrs := ActivityAttributes(activity.Info{Attempt: 3, ScheduledTime: now, StartedTime: now.Add(5 * time.Second), RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 9}})
	values := map[string]any{}
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value.AsInterface()
	}
	if values["temporal.activity.attempt"] != int64(3) || values["temporal.activity.retry"] != true || values["temporal.activity.schedule_to_start_seconds"] != float64(5) {
		t.Fatal(values)
	}
	if attrs := ActivityAttributes(activity.Info{ScheduledTime: now, StartedTime: now.Add(-time.Second)}); attrs[len(attrs)-1].Value.AsFloat64() != 0 {
		t.Fatal("negative delay")
	}
	recorder := tracetest.NewSpanRecorder()
	p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer p.Shutdown(context.Background())
	_, span := p.Tracer("test").Start(context.Background(), "cancelled")
	safe := &safeSpan{Span: span, ctx: context.Background()}
	safe.RecordError(temporal.NewCanceledError("PRIVATE"))
	safe.SetStatus(codes.Error, "PRIVATE")
	safe.End()
	if recorder.Ended()[0].Status().Code == codes.Error {
		t.Fatal("cancellation failed")
	}
}

func TestSDKCompletionClassesWithoutStaleErrors(t *testing.T) {
	for _, kind := range []string{"error", "benign", "cancelled", "deferred"} {
		t.Run(kind, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			p := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			old := otel.GetTracerProvider()
			otel.SetTracerProvider(p)
			defer func() { otel.SetTracerProvider(old); p.Shutdown(context.Background()) }()
			ctx := context.Background()
			err := errors.New("PRIVATE payload")
			switch kind {
			case "benign":
				err = temporal.NewApplicationErrorWithOptions("PRIVATE payload", "test", temporal.ApplicationErrorOptions{Category: temporal.ApplicationErrorCategoryBenign})
			case "cancelled":
				err = temporal.NewCanceledError("PRIVATE payload")
			case "deferred":
				var cancel context.CancelFunc
				ctx, cancel = outcome.WithWorkBudget(ctx, -time.Second)
				defer cancel()
				err = context.DeadlineExceeded
			}
			tracing, createErr := New(Config{})
			if createErr != nil {
				t.Fatal(createErr)
			}
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestActivityEnvironment()
			env.SetWorkerOptions(worker.Options{BackgroundActivityContext: ctx, Interceptors: []interceptor.WorkerInterceptor{tracing}})
			fn := func(context.Context) error { return err }
			env.RegisterActivity(fn)
			_, _ = env.ExecuteActivity(fn)
			if len(recorder.Ended()) != 1 {
				t.Fatal("missing actual execution")
			}
			span := recorder.Ended()[0]
			if (span.Status().Code == codes.Error) != (kind == "error") {
				t.Fatal(kind, span.Status())
			}
			for _, a := range span.Attributes() {
				if a.Key == "error.type" && kind != "error" {
					t.Fatal("stale error type", kind)
				}
			}
			if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "PRIVATE") {
				t.Fatal("exception text leaked")
			}
		})
	}
}
