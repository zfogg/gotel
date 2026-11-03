// Package temporal configures Temporal's OpenTelemetry interceptor with
// stable, application-prefixed names that expose workflow and activity types.
package temporal

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/zfogg/gotel/go"
	"github.com/zfogg/gotel/go/outcome"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	otelcontrib "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	sdktemporal "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type Config struct {
	// Application owns instrumentation scope and optional span-name prefix.
	Application gotel.Application
	// AllowInvalidParentSpans follows the SDK migration option; false rejects malformed propagated parents.
	AllowInvalidParentSpans bool
	// ExecutionLinks supplies explicit prior-attempt/execution links. At most 16
	// are retained per execution; no workflow/activity IDs enter metric labels.
	ExecutionLinks func(context.Context) []trace.Link
	// WorkflowExecutionLinks supplies explicit prior workflow execution links.
	// SDK workflow contexts are deterministic; no history is retained by Gotel.
	WorkflowExecutionLinks func(workflow.Context) []trace.Link
}

// ConfigureClient installs Temporal tracing once, on the client. Temporal's
// combined interceptor is also a WorkerInterceptor and the SDK carries it into
// workers created from that client. Registering it again in worker.Options
// produces duplicate, identically timed workflow and activity spans.
func ConfigureClient(options client.Options, config Config) (client.Options, error) {
	tracing, err := New(config)
	if err != nil {
		return options, err
	}
	options.Interceptors = append(options.Interceptors, tracing)
	return options, nil
}

// New creates one combined SDK interceptor plus metadata decorators. Register
// once on a client; client workers inherit it. SDK validation errors are returned.
func New(config Config) (interceptor.Interceptor, error) {
	key := &workflowSpanKey{}
	tracing, err := otelcontrib.NewTracingInterceptor(otelcontrib.TracerOptions{
		Tracer:                  config.Application.Tracer("temporal"),
		SpanContextKey:          key,
		TextMapPropagator:       otel.GetTextMapPropagator(),
		AllowInvalidParentSpans: config.AllowInvalidParentSpans,
		SpanStarter: func(ctx context.Context, tracer trace.Tracer, raw string, options ...trace.SpanStartOption) trace.Span {
			if strings.HasPrefix(raw, "RunActivity:") || strings.HasPrefix(raw, "RunWorkflow:") {
				if parent := trace.SpanContextFromContext(ctx); parent.IsValid() {
					options = append(options, trace.WithLinks(trace.Link{SpanContext: parent}))
				}
			}
			_, span := tracer.Start(ctx, Name(raw), options...)
			return &safeSpan{Span: span, ctx: ctx}
		},
	})
	if err != nil {
		return nil, err
	}
	return &enrichedInterceptor{Interceptor: tracing, key: key, config: config}, nil
}

type workflowSpanKey struct{ marker byte }
type enrichedInterceptor struct {
	interceptor.Interceptor
	key    *workflowSpanKey
	config Config
}

func (t *enrichedInterceptor) InterceptActivity(ctx context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return t.Interceptor.InterceptActivity(ctx, &activityMetadata{ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next}, config: t.config})
}
func (t *enrichedInterceptor) InterceptWorkflow(ctx workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return t.Interceptor.InterceptWorkflow(ctx, &workflowMetadata{WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next}, key: t.key, config: t.config})
}

type activityMetadata struct {
	interceptor.ActivityInboundInterceptorBase
	config Config
}

func (t *activityMetadata) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (any, error) {
	span := trace.SpanFromContext(ctx)
	if safe, ok := span.(*safeSpan); ok {
		safe.mu.Lock()
		safe.ctx = ctx
		safe.mu.Unlock()
	}
	span.SetAttributes(ActivityAttributes(activity.GetInfo(ctx))...)
	if t.config.ExecutionLinks != nil {
		links := t.config.ExecutionLinks(ctx)
		if len(links) > 15 {
			links = links[:15]
		}
		for _, link := range links {
			span.AddLink(link)
		}
	}
	return t.Next.ExecuteActivity(ctx, in)
}

type workflowMetadata struct {
	interceptor.WorkflowInboundInterceptorBase
	key    *workflowSpanKey
	config Config
}

func (t *workflowMetadata) ExecuteWorkflow(ctx workflow.Context, in *interceptor.ExecuteWorkflowInput) (any, error) {
	if !workflow.IsReplaying(ctx) {
		if span, ok := ctx.Value(t.key).(interface {
			SetAttributes(...attribute.KeyValue)
			AddLink(trace.Link)
		}); ok {
			info := workflow.GetInfo(ctx)
			span.SetAttributes(attribute.Int("temporal.workflow.attempt", int(info.Attempt)), attribute.Bool("temporal.workflow.retry", info.Attempt > 1), attribute.String("temporal.workflow.id", info.WorkflowExecution.ID), attribute.String("temporal.workflow.run_id", info.WorkflowExecution.RunID))
			if t.config.WorkflowExecutionLinks != nil {
				links := t.config.WorkflowExecutionLinks(ctx)
				if len(links) > 15 {
					links = links[:15]
				}
				for _, link := range links {
					span.AddLink(link)
				}
			}
		}
	}
	return t.Next.ExecuteWorkflow(ctx, in)
}

// The SDK's default Finish records raw exception/status text. Keep its replay
// ownership while restricting errors to a type and explicit completion outcome.
type safeSpan struct {
	trace.Span
	mu     sync.Mutex
	ctx    context.Context
	result outcome.Value
	err    error
	status codes.Code
}

func (s *safeSpan) RecordError(err error, _ ...trace.EventOption) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	s.result = outcome.Classify(s.ctx, err)
	var applicationError *sdktemporal.ApplicationError
	if errors.As(err, &applicationError) && applicationError.Category() == sdktemporal.ApplicationErrorCategoryBenign {
		s.result = outcome.Success
	}
	if sdktemporal.IsCanceledError(err) {
		s.result = outcome.Cancelled
	}
}
func (s *safeSpan) SetStatus(code codes.Code, _ string) {
	s.mu.Lock()
	s.status = code
	s.mu.Unlock()
}
func (s *safeSpan) End(options ...trace.SpanEndOption) {
	s.mu.Lock()
	result, err := s.result, s.err
	if result == "" {
		result = outcome.Success
		if s.status == codes.Error {
			result = outcome.Error
		}
	}
	s.mu.Unlock()
	outcome.Apply(s.Span, result, err)
	s.Span.End(options...)
}

// ActivityAttributes describes one server execution attempt. Go's ScheduledTime
// is the original schedule time, so delay includes retry backoff on later attempts.
// No payloads, task tokens, exception messages, or heartbeat details are recorded.
func ActivityAttributes(info activity.Info) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.Int("temporal.activity.attempt", int(info.Attempt)),
		attribute.Bool("temporal.activity.retry", info.Attempt > 1),
		attribute.String("temporal.activity.id", info.ActivityID),
		attribute.String("temporal.workflow.id", info.WorkflowExecution.ID),
		attribute.String("temporal.workflow.run_id", info.WorkflowExecution.RunID),
	}
	if !info.ScheduledTime.IsZero() && !info.StartedTime.IsZero() {
		delay := info.StartedTime.Sub(info.ScheduledTime).Seconds()
		if delay < 0 {
			delay = 0
		}
		attrs = append(attrs, attribute.Float64("temporal.activity.schedule_to_start_seconds", delay))
	}
	if info.RetryPolicy != nil {
		attrs = append(attrs, attribute.Int("temporal.activity.retry.maximum_attempts", int(info.RetryPolicy.MaximumAttempts)))
	}
	return attrs
}

// Name translates Temporal SDK Operation:Type names into stable operation
// families while retaining the actual workflow/activity name. Scheduling an
// activity and executing it are separate operations, so they must not share a
// span name even when a worker and workflow client run in the same process.
func Name(raw string) string {
	operation, name, ok := strings.Cut(raw, ":")
	if !ok {
		return "temporal." + strings.ToLower(raw)
	}
	kind := strings.ToLower(operation)
	switch operation {
	case "RunWorkflow", "StartWorkflow":
		kind = "workflow"
	case "StartActivity":
		kind = "activity.schedule"
	case "RunActivity":
		kind = "activity.execute"
	case "RunChildWorkflow", "StartChildWorkflow":
		kind = "workflow.child"
	}
	return "temporal." + kind + "." + name
}
