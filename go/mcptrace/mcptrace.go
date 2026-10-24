// Package mcptrace instruments the official Go MCP SDK's receiving middleware.
// Arguments, results, error messages, and request identifiers are never recorded.
package mcptrace

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Options fixes the instrumentation's names and bounded tool vocabulary at setup.
type Options struct {
	// Prefix qualifies spans and metrics, for example "spaniel". Empty gives mcp.*.
	Prefix string
	// Tools is the registered tool vocabulary. Unlisted names become "unknown".
	Tools          []string
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	// Context optionally adds application context before starting the span.
	Context func(context.Context) context.Context
}

// New returns SDK middleware. Install once with Server.AddReceivingMiddleware.
// Construct after providers are installed, or use the global delegate providers.
func New(opts Options) (mcp.Middleware, error) {
	prefix := strings.Trim(strings.TrimSpace(opts.Prefix), ".")
	qualify := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}
	scope := strings.Trim(prefix+"/mcp", "/")
	tp, mp := opts.TracerProvider, opts.MeterProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	tracer, meter := tp.Tracer(scope), mp.Meter(scope)
	requests, err := meter.Int64Counter(qualify("mcp.requests"), metric.WithDescription("MCP requests by method, tool, and result"), metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram(qualify("mcp.duration"), metric.WithDescription("MCP request duration"), metric.WithUnit("ms"))
	if err != nil {
		return nil, err
	}
	tools := make(map[string]bool, len(opts.Tools))
	for _, name := range opts.Tools {
		tools[name] = true
	}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if opts.Context != nil {
				ctx = opts.Context(ctx)
			}
			started := time.Now()
			methodLabel := boundedMethod(method)
			name, tool := "mcp."+methodLabel, ""
			if req != nil {
				switch p := req.GetParams().(type) {
				case *mcp.CallToolParamsRaw:
					if p != nil {
						tool = p.Name
					}
				case *mcp.CallToolParams:
					if p != nil {
						tool = p.Name
					}
				}
			}
			if method == "tools/call" || tool != "" {
				if !tools[tool] {
					tool = "unknown"
				}
				name = "mcp.tool/" + tool
			}
			ctx, span := tracer.Start(ctx, qualify(name), trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("rpc.system", "mcp"), attribute.String("rpc.method", methodLabel)))
			if tool != "" {
				span.SetAttributes(attribute.String("gen_ai.tool.name", tool))
			}
			outcome := "ok"
			defer func() {
				if recovered := recover(); recovered != nil {
					outcome = "error"
					finish(ctx, span, requests, duration, methodLabel, tool, outcome, started)
					panic(recovered)
				}
				finish(ctx, span, requests, duration, methodLabel, tool, outcome, started)
			}()
			res, err := next(ctx, method, req)
			toolError := false
			if result, ok := res.(*mcp.CallToolResult); ok && result != nil {
				toolError = result.IsError
			}
			if err != nil || toolError {
				outcome = "error"
			}
			return res, err
		}
	}, nil
}

func finish(ctx context.Context, span trace.Span, requests metric.Int64Counter, duration metric.Float64Histogram, method, tool, outcome string, started time.Time) {
	defer span.End()
	if outcome == "error" {
		span.SetStatus(codes.Error, "MCP request failed")
		span.SetAttributes(attribute.String("error.type", "mcp_error"))
	}
	span.SetAttributes(attribute.Bool("mcp.is_error", outcome == "error"))
	attrs := metric.WithAttributes(attribute.String("method", method), attribute.String("tool", tool), attribute.String("result", outcome))
	requests.Add(ctx, 1, attrs)
	duration.Record(ctx, float64(time.Since(started))/float64(time.Millisecond), attrs)
}

func boundedMethod(method string) string {
	switch method {
	case "initialize", "ping", "tools/list", "tools/call", "resources/list", "resources/read", "resources/templates/list", "resources/subscribe", "resources/unsubscribe", "prompts/list", "prompts/get", "completion/complete", "logging/setLevel", "notifications/initialized", "notifications/cancelled", "notifications/progress", "notifications/roots/list_changed":
		return method
	default:
		return "other"
	}
}
