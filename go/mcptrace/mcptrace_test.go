package mcptrace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMiddleware(t *testing.T) {
	for _, outcome := range []string{"ok", "protocol", "tool", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			reader := sdkmetric.NewManualReader()
			mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()); _ = mp.Shutdown(context.Background()) })
			type contextKey struct{}
			middleware, err := New(Options{Prefix: "demo", Tools: []string{"lookup"}, TracerProvider: tp, MeterProvider: mp, Context: func(ctx context.Context) context.Context { return context.WithValue(ctx, contextKey{}, true) }})
			if err != nil {
				t.Fatal(err)
			}
			handler := middleware(func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				if ctx.Value(contextKey{}) != true || method != "tools/call" {
					t.Fatal("context or method lost")
				}
				_, child := tp.Tracer("storage").Start(ctx, "query")
				child.End()
				switch outcome {
				case "protocol":
					return nil, errors.New("secret error")
				case "tool":
					return &mcp.CallToolResult{IsError: true}, nil
				case "panic":
					panic("secret panic")
				}
				return &mcp.CallToolResult{}, nil
			})
			func() {
				defer func() {
					if value := recover(); (value != nil) != (outcome == "panic") {
						t.Fatalf("panic=%v", value)
					}
				}()
				_, err = handler(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "lookup", Arguments: []byte(`{"password":"secret"}`)}})
				if (err != nil) != (outcome == "protocol") {
					t.Fatalf("error=%v", err)
				}
			}()
			spans := exporter.GetSpans()
			if len(spans) != 2 {
				t.Fatalf("spans=%d", len(spans))
			}
			root := spans[1]
			if root.Name != "demo.mcp.tool/lookup" || spans[0].Parent.SpanID() != root.SpanContext.SpanID() {
				t.Fatalf("broken naming/parent: %+v", spans)
			}
			if (root.Status.Code == codes.Error) != (outcome != "ok") {
				t.Fatalf("status=%v", root.Status)
			}
			for _, a := range root.Attributes {
				if strings.Contains(a.Value.Emit(), "secret") {
					t.Fatal("sensitive content recorded")
				}
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &data); err != nil {
				t.Fatal(err)
			}
			if len(data.ScopeMetrics) != 1 || data.ScopeMetrics[0].Scope.Name != "demo/mcp" {
				t.Fatalf("scope=%+v", data)
			}
			for _, m := range data.ScopeMetrics[0].Metrics {
				switch d := m.Data.(type) {
				case metricdata.Sum[int64]:
					if m.Name != "demo.mcp.requests" || len(d.DataPoints) != 1 || d.DataPoints[0].Value != 1 {
						t.Fatalf("counter=%+v", m)
					}
					result, _ := d.DataPoints[0].Attributes.Value(attribute.Key("result"))
					if (result.AsString() == "error") != (outcome != "ok") {
						t.Fatal("wrong metric outcome")
					}
				case metricdata.Histogram[float64]:
					if m.Name != "demo.mcp.duration" || len(d.DataPoints) != 1 || d.DataPoints[0].Count != 1 {
						t.Fatalf("histogram=%+v", m)
					}
				default:
					t.Fatalf("unexpected metric: %+v", m)
				}
			}
		})
	}
}

func TestUnknownNamesAreBounded(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer tp.Shutdown(context.Background())
	middleware, err := New(Options{TracerProvider: tp})
	if err != nil {
		t.Fatal(err)
	}
	next := middleware(func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, nil })
	_, _ = next(context.Background(), "secret-method", nil)
	_, _ = next(context.Background(), "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "secret-tool"}})
	spans := exporter.GetSpans()
	if len(spans) != 2 || spans[0].Name != "mcp.other" || spans[1].Name != "mcp.tool/unknown" {
		t.Fatalf("unbounded names: %+v", spans)
	}
}
