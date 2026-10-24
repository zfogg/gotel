package gotel

import (
	"context"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"strings"
)

// WithoutApplicationPrefix omits one application prefix from exported span
// names. It preserves the original spans, resources, scopes and metric names.
// Use it with custom processors; otelhost.Config can install it automatically.
func WithoutApplicationPrefix(exporter sdktrace.SpanExporter, application string) sdktrace.SpanExporter {
	prefix := strings.TrimRight(strings.TrimSpace(application), ".")
	if prefix == "" || exporter == nil {
		return exporter
	}
	return &spanNameExporter{SpanExporter: exporter, prefix: prefix + "."}
}

type spanNameExporter struct {
	sdktrace.SpanExporter
	prefix string
}
type namedSpan struct {
	sdktrace.ReadOnlySpan
	name string
}

func (s namedSpan) Name() string { return s.name }
func (e *spanNameExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	output := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, span := range spans {
		name := strings.TrimPrefix(span.Name(), e.prefix)
		if name == "" {
			name = span.Name()
		}
		output[i] = namedSpan{ReadOnlySpan: span, name: name}
	}
	return e.SpanExporter.ExportSpans(ctx, output)
}
