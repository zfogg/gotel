package dbmetrics

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecorderEmitsStandardDurationMetric(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	recorder, err := New(Config{Meter: provider.Meter("test"), System: "postgresql", Namespace: "app"})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Observe(t.Context(), "SELECT", "ok", 25*time.Millisecond)
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	if len(metrics.ScopeMetrics) != 1 || len(metrics.ScopeMetrics[0].Metrics) != 1 {
		t.Fatalf("metrics = %#v", metrics.ScopeMetrics)
	}
	got := metrics.ScopeMetrics[0].Metrics[0]
	if got.Name != DefaultHistogramName || got.Unit != "s" {
		t.Fatalf("metric = %q %q", got.Name, got.Unit)
	}
	histogram, ok := got.Data.(metricdata.Histogram[float64])
	if !ok || len(histogram.DataPoints) != 1 || histogram.DataPoints[0].Count != 1 {
		t.Fatalf("histogram = %#v", got.Data)
	}
}

func TestChainCallsEveryObserver(t *testing.T) {
	called := 0
	observer := func(context.Context, string, string, time.Duration) { called++ }
	Chain(observer, nil, observer)(context.Background(), "SELECT", "ok", time.Second)
	if called != 2 {
		t.Fatalf("called = %d", called)
	}
}
