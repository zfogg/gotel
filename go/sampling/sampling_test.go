package sampling

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zfogg/gotel/go/outcome"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestCriticalMatcherAndStartOnly(t *testing.T) {
	matcher := Matcher{NameContains: []string{"broker."}, TemporalFamilies: []string{"Backtest"}, Classes: []string{"failure"}}
	p, err := New(Config{Delegate: sdktrace.NeverSample(), Critical: matcher.Match})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app.broker.submit", "RunWorkflow:BacktestWorkflow", "app.temporal.activity.execute.Backtest"} {
		if p.ShouldSample(sdktrace.SamplingParameters{ParentContext: context.Background(), Name: name}).Decision != sdktrace.RecordAndSample {
			t.Fatal(name)
		}
	}
	if !matcher.Match(sdktrace.SamplingParameters{Attributes: []attribute.KeyValue{attribute.String("operation.class", "FAILURE")}}) {
		t.Fatal("class")
	}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(p), sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())
	_, span := provider.Tracer("test").Start(context.Background(), "ordinary")
	span.SetAttributes(attribute.String("operation.class", "failure"))
	span.End()
	if len(recorder.Ended()) != 0 {
		t.Fatal("late attribute retroactively sampled")
	}
}

func TestRealtimeCapParentAndConcurrentBound(t *testing.T) {
	now := time.Unix(100, 0)
	p, err := New(Config{Delegate: sdktrace.NeverSample(), Realtime: func(sdktrace.SamplingParameters) bool { return true }, RealtimeRate: 1, RealtimeMaxPerSecond: 7, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p.ShouldSample(sdktrace.SamplingParameters{ParentContext: context.Background()}).Decision == sdktrace.RecordAndSample {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 7 {
		t.Fatalf("cap=%d", count.Load())
	}
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled})
	if p.ShouldSample(sdktrace.SamplingParameters{ParentContext: trace.ContextWithSpanContext(context.Background(), parent)}).Decision != sdktrace.RecordAndSample {
		t.Fatal("child re-rolled")
	}
	parent = parent.WithTraceFlags(0)
	if p.ShouldSample(sdktrace.SamplingParameters{ParentContext: trace.ContextWithSpanContext(context.Background(), parent)}).Decision != sdktrace.Drop {
		t.Fatal("dropped decision lost")
	}
	now = now.Add(time.Second)
	if p.ShouldSample(sdktrace.SamplingParameters{ParentContext: context.Background()}).Decision != sdktrace.RecordAndSample {
		t.Fatal("cap did not reset")
	}
	for _, rate := range []float64{-1, 2, math.NaN(), math.Inf(1)} {
		if _, err := New(Config{RealtimeRate: rate}); err == nil {
			t.Fatal("invalid rate accepted")
		}
	}
}

func TestRetainedObservationIndependentLinkedBounded(t *testing.T) {
	policy, _ := New(Config{Delegate: sdktrace.NeverSample()})
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(policy), sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())
	ctx, original := provider.Tracer("test").Start(context.Background(), "dropped")
	now := time.Now()
	r := Retainer{Tracer: provider.Tracer("test"), MaxPerSecond: 2, Now: func() time.Time { return now }}
	for i := range 3 {
		if r.Observe(ctx, "failure", outcome.Error, time.Millisecond) != (i < 2) {
			t.Fatal("retention cap")
		}
	}
	if r.Observe(ctx, "success", outcome.Success, 0) {
		t.Fatal("retained success")
	}
	original.End()
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatal(len(spans))
	}
	for _, span := range spans {
		if span.Parent().IsValid() || span.SpanContext().TraceID() == original.SpanContext().TraceID() {
			t.Fatal("original head decision reused")
		}
		if len(span.Links()) != 1 || span.Links()[0].SpanContext.TraceID() != original.SpanContext().TraceID() || span.Links()[0].SpanContext.SpanID() != original.SpanContext().SpanID() {
			t.Fatal("missing execution link")
		}
		if span.EndTime().Sub(span.StartTime()) != time.Millisecond {
			t.Fatal("duration")
		}
	}
}
