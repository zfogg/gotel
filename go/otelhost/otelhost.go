// Package otelhost configures a complete OpenTelemetry SDK host while leaving
// application-specific metrics, views, and sampling policy configurable.
package otelhost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/zfogg/gotel/go"
	"github.com/zfogg/gotel/go/traceprocessor"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	goruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Protocol selects the OTLP transport.
type Protocol string

const (
	ProtocolGRPC         Protocol = "grpc"
	ProtocolHTTPProtobuf Protocol = "http/protobuf"
)

// Signals selects SDK pipelines. The zero value enables all signals.
type Signals uint8

const (
	Traces Signals = 1 << iota
	Metrics
	Logs
	AllSignals = Traces | Metrics | Logs
)

// SlogConfig installs an OTLP-aware slog handler, optionally fanned out to
// local handlers. Attributes are attached to every application log.
type SlogConfig struct {
	Scope      string
	Attributes []slog.Attr
	Handlers   []slog.Handler
}

// Config describes resources, exporters, propagation, and extension points.
// Injected exporters/readers make setup deterministic in tests.
type Config struct {
	// DisableSpanNamePrefix omits ApplicationName + "." from exported span
	// names. ApplicationName defaults to ServiceName. Metrics/scopes are unchanged.
	DisableSpanNamePrefix bool
	ApplicationName       string
	ServiceName           string
	ServiceVersion        string
	Environment           string
	Endpoint              string
	Protocol              Protocol
	Insecure              bool
	Headers               map[string]string
	Signals               Signals
	Attributes            []attribute.KeyValue
	// ResourceOptions extend the standard SDK/host/OS/process resource. Use
	// resource.WithFromEnv to honor OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME.
	ResourceOptions []resource.Option
	Sampler         sdktrace.Sampler
	MetricInterval  time.Duration
	Propagator      propagation.TextMapPropagator
	TraceExporter   sdktrace.SpanExporter
	MetricReader    sdkmetric.Reader
	LogExporter     sdklog.Exporter
	TraceOptions    []sdktrace.TracerProviderOption
	MetricOptions   []sdkmetric.Option
	LogOptions      []sdklog.LoggerProviderOption
	Slog            *SlogConfig
	RuntimeMetrics  bool
}

var runtimeMetricsOnce sync.Once

// Runtime owns providers created by Start.
type Runtime struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
	shutdown       func(context.Context) error
}

// Shutdown flushes and closes all configured providers in reverse order.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || r.shutdown == nil {
		return nil
	}
	return r.shutdown(ctx)
}

// Start constructs selected SDK pipelines and installs their global providers.
func Start(ctx context.Context, config Config) (_ *Runtime, err error) {
	if strings.TrimSpace(config.ServiceName) == "" {
		return nil, fmt.Errorf("otelhost: ServiceName is required")
	}
	if config.Protocol == "" {
		config.Protocol = ProtocolGRPC
	}
	if config.Signals == 0 {
		config.Signals = AllSignals
	}
	if config.MetricInterval <= 0 {
		config.MetricInterval = 10 * time.Second
	}
	res, err := NewResource(ctx, ResourceConfig{ServiceName: config.ServiceName, ServiceVersion: config.ServiceVersion, Environment: config.Environment, Attributes: config.Attributes, Options: config.ResourceOptions})
	if err != nil && res == nil {
		return nil, err
	}
	runtime := &Runtime{}
	stack := &gotel.ShutdownStack{}
	fail := func(cause error) (*Runtime, error) { _ = stack.Shutdown(ctx); return nil, cause }

	if config.Signals&Traces != 0 {
		exporter := config.TraceExporter
		if exporter == nil && config.Endpoint != "" {
			exporter, err = traceExporter(ctx, config)
			if err != nil {
				return fail(err)
			}
		}
		opts := append([]sdktrace.TracerProviderOption{sdktrace.WithResource(res)}, config.TraceOptions...)
		if config.Sampler != nil {
			opts = append(opts, sdktrace.WithSampler(config.Sampler))
		}
		if exporter != nil {
			if config.DisableSpanNamePrefix {
				application := config.ApplicationName
				if application == "" {
					application = config.ServiceName
				}
				exporter = gotel.WithoutApplicationPrefix(exporter, application)
			}
			// Export a completed local trace as one batch. The SDK span batcher
			// publishes finished children before a long-running parent ends,
			// leaving collectors with orphan rows and a growing waterfall.
			application := config.ApplicationName
			if application == "" {
				application = config.ServiceName
			}
			opts = append(opts, sdktrace.WithSpanProcessor(traceprocessor.New(traceprocessor.Config{
				Application: gotel.NewApplication(application), Exporter: exporter,
			})))
		}
		runtime.TracerProvider = sdktrace.NewTracerProvider(opts...)
		otel.SetTracerProvider(runtime.TracerProvider)
		stack.Add(runtime.TracerProvider.Shutdown)
	}
	if config.Signals&Metrics != 0 {
		reader := config.MetricReader
		if reader == nil && config.Endpoint != "" {
			var exporter sdkmetric.Exporter
			exporter, err = metricExporter(ctx, config)
			if err != nil {
				return fail(err)
			}
			reader = sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(config.MetricInterval))
		}
		opts := append([]sdkmetric.Option{sdkmetric.WithResource(res)}, config.MetricOptions...)
		if reader != nil {
			opts = append(opts, sdkmetric.WithReader(reader))
		}
		runtime.MeterProvider = sdkmetric.NewMeterProvider(opts...)
		otel.SetMeterProvider(runtime.MeterProvider)
		stack.Add(runtime.MeterProvider.Shutdown)
	}
	if config.Signals&Logs != 0 {
		exporter := config.LogExporter
		if exporter == nil && config.Endpoint != "" {
			exporter, err = logExporter(ctx, config)
			if err != nil {
				return fail(err)
			}
		}
		opts := append([]sdklog.LoggerProviderOption{sdklog.WithResource(res)}, config.LogOptions...)
		if exporter != nil {
			opts = append(opts, sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
		}
		runtime.LoggerProvider = sdklog.NewLoggerProvider(opts...)
		global.SetLoggerProvider(runtime.LoggerProvider)
		stack.Add(runtime.LoggerProvider.Shutdown)
		if config.Slog != nil {
			scope := config.Slog.Scope
			if scope == "" {
				scope = config.ServiceName
			}
			otelHandler := otelslog.NewHandler(scope).WithAttrs(config.Slog.Attributes)
			handlers := append([]slog.Handler{otelHandler}, config.Slog.Handlers...)
			slog.SetDefault(slog.New(gotel.NewFanoutHandler(handlers...)))
		}
	}
	propagator := config.Propagator
	if propagator == nil {
		propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}
	otel.SetTextMapPropagator(propagator)
	if config.RuntimeMetrics {
		runtimeMetricsOnce.Do(func() {
			if err := goruntime.Start(); err != nil {
				otel.Handle(err)
			}
		})
	}
	runtime.shutdown = stack.Shutdown
	return runtime, nil
}

func traceExporter(ctx context.Context, c Config) (sdktrace.SpanExporter, error) {
	if c.Protocol == ProtocolHTTPProtobuf {
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(c.Endpoint), otlptracehttp.WithHeaders(c.Headers)}
		if c.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, opts...)
	}
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(c.Endpoint), otlptracegrpc.WithHeaders(c.Headers)}
	if c.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, opts...)
}
func metricExporter(ctx context.Context, c Config) (sdkmetric.Exporter, error) {
	if c.Protocol == ProtocolHTTPProtobuf {
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(c.Endpoint), otlpmetrichttp.WithHeaders(c.Headers)}
		if c.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		return otlpmetrichttp.New(ctx, opts...)
	}
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(c.Endpoint), otlpmetricgrpc.WithHeaders(c.Headers)}
	if c.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	return otlpmetricgrpc.New(ctx, opts...)
}
func logExporter(ctx context.Context, c Config) (sdklog.Exporter, error) {
	if c.Protocol == ProtocolHTTPProtobuf {
		opts := []otlploghttp.Option{otlploghttp.WithEndpointURL(c.Endpoint), otlploghttp.WithHeaders(c.Headers)}
		if c.Insecure {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		return otlploghttp.New(ctx, opts...)
	}
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(c.Endpoint), otlploggrpc.WithHeaders(c.Headers)}
	if c.Insecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	return otlploggrpc.New(ctx, opts...)
}
