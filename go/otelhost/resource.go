package otelhost

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type ResourceConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Attributes     []attribute.KeyValue
	Options        []resource.Option
}

// NewResource applies the same host/process/service identity to every gotel
// application. Environment options remain explicit so callers choose whether
// OTEL_RESOURCE_ATTRIBUTES may override deployment configuration.
func NewResource(ctx context.Context, config ResourceConfig) (*resource.Resource, error) {
	attrs := append([]attribute.KeyValue{semconv.ServiceName(config.ServiceName)}, config.Attributes...)
	if config.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(config.ServiceVersion))
	}
	if config.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", config.Environment))
	}
	options := []resource.Option{resource.WithTelemetrySDK(), resource.WithHost(), resource.WithOS(), resource.WithProcess(), resource.WithAttributes(attrs...)}
	options = append(options, config.Options...)
	return resource.New(ctx, options...)
}
