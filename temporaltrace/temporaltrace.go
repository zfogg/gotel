// Package temporaltrace configures Temporal's OpenTelemetry interceptor with
// stable, application-prefixed names that expose workflow and activity types.
package temporaltrace

import (
	"context"
	"strings"

	"github.com/zfogg/gotel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	otelcontrib "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
)

type Config struct {
	Application             gotel.Application
	AllowInvalidParentSpans bool
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

func New(config Config) (interceptor.Interceptor, error) {
	return otelcontrib.NewTracingInterceptor(otelcontrib.TracerOptions{
		Tracer:                  config.Application.Tracer("temporal"),
		TextMapPropagator:       otel.GetTextMapPropagator(),
		AllowInvalidParentSpans: config.AllowInvalidParentSpans,
		SpanStarter: func(ctx context.Context, tracer trace.Tracer, raw string, options ...trace.SpanStartOption) trace.Span {
			_, span := tracer.Start(ctx, Name(raw), options...)
			return span
		},
	})
}

// Name translates Temporal SDK Operation:Type names into stable operation
// families while retaining the actual workflow/activity name.
func Name(raw string) string {
	operation, name, ok := strings.Cut(raw, ":")
	if !ok {
		return "temporal." + strings.ToLower(raw)
	}
	kind := strings.ToLower(operation)
	switch operation {
	case "RunWorkflow", "StartWorkflow":
		kind = "workflow"
	case "RunActivity", "StartActivity":
		kind = "activity"
	case "RunChildWorkflow", "StartChildWorkflow":
		kind = "workflow.child"
	}
	return "temporal." + kind + "." + name
}
