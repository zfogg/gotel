// Package dbmetrics provides reusable, low-cardinality database client metrics
// compatible with sqltrace, pgxtrace, and gormtrace observers.
package dbmetrics

import (
	"context"
	"time"

	"github.com/zfogg/gotel/sqltrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// DefaultHistogramName is the OpenTelemetry database client duration metric.
	DefaultHistogramName = "db.client.operation.duration"
)

var resultKey = attribute.Key("gotel.db.result")

// Config controls database duration metric construction and resource-stable
// attributes. The zero meter uses the global MeterProvider.
type Config struct {
	Meter         metric.Meter
	Scope         string
	HistogramName string
	Description   string
	System        string
	Namespace     string
	Attributes    []attribute.KeyValue
}

// Recorder owns a database duration histogram and exposes a sqltrace.Observer.
type Recorder struct {
	duration   metric.Float64Histogram
	attributes []attribute.KeyValue
}

// New constructs a database metric recorder using seconds as the duration unit.
func New(config Config) (*Recorder, error) {
	if config.Scope == "" {
		config.Scope = "gotel/dbmetrics"
	}
	if config.Meter == nil {
		config.Meter = otel.Meter(config.Scope)
	}
	if config.HistogramName == "" {
		config.HistogramName = DefaultHistogramName
	}
	if config.Description == "" {
		config.Description = "Duration of database client operations."
	}
	duration, err := config.Meter.Float64Histogram(config.HistogramName,
		metric.WithUnit("s"), metric.WithDescription(config.Description))
	if err != nil {
		return nil, err
	}
	attrs := append([]attribute.KeyValue(nil), config.Attributes...)
	if config.System != "" {
		attrs = append(attrs, sqltrace.SystemKey.String(config.System))
	}
	if config.Namespace != "" {
		attrs = append(attrs, sqltrace.NamespaceKey.String(config.Namespace))
	}
	return &Recorder{duration: duration, attributes: attrs}, nil
}

// Must constructs a database metric recorder and panics if instrument
// registration fails. It is intended for package-level instrumentation.
func Must(config Config) *Recorder {
	recorder, err := New(config)
	if err != nil {
		panic(err)
	}
	return recorder
}

// Observe records one database operation. Operation and result are bounded
// labels supplied by gotel adapters; query text and bind values are never used.
func (r *Recorder) Observe(ctx context.Context, operation, result string, elapsed time.Duration) {
	if r == nil {
		return
	}
	attrs := make([]attribute.KeyValue, 0, len(r.attributes)+2)
	attrs = append(attrs, r.attributes...)
	attrs = append(attrs, sqltrace.OperationKey.String(operation), resultKey.String(result))
	r.duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attrs...))
}

// Observer returns Observe as a sqltrace.Observer.
func (r *Recorder) Observer() sqltrace.Observer { return r.Observe }

// Chain calls each non-nil observer in order. It lets an application retain a
// domain-specific catalog metric alongside gotel's standard duration metric.
func Chain(observers ...sqltrace.Observer) sqltrace.Observer {
	return func(ctx context.Context, operation, result string, elapsed time.Duration) {
		for _, observer := range observers {
			if observer != nil {
				observer(ctx, operation, result, elapsed)
			}
		}
	}
}
