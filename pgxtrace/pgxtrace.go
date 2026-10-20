// Package pgxtrace instruments pgx database operations using sqltrace's stable
// names and sanitized query templates.
package pgxtrace

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/zfogg/gotel"
	"github.com/zfogg/gotel/sqltrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config controls pgx database span names and attributes.
type Config struct {
	// Scope is the OpenTelemetry instrumentation scope.
	Scope string
	// Namer validates and prefixes every database span name.
	Namer sqltrace.Namer
	// System is db.system.name and defaults to postgresql.
	System string
	// Namespace is the logical database name recorded as db.namespace.
	Namespace string
}

// Tracer implements pgx query and copy tracing with safe query metadata.
type Tracer struct {
	config Config
	tracer trace.Tracer
}

// New constructs a pgx tracer from config.
func New(config Config) *Tracer {
	if config.Scope == "" {
		config.Scope = "gotel/pgx"
	}
	if config.System == "" {
		config.System = "postgresql"
	}
	return &Tracer{config: config, tracer: otel.Tracer(config.Scope)}
}

type spanKey struct{}

func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if sqltrace.TracingDisabled(ctx) {
		return ctx
	}
	explicit := sqltrace.NameFromContext(ctx)
	if explicit != "" {
		// Inspect accepts simple names; context names are already qualified.
		explicit = ""
	}
	meta := sqltrace.Inspect(t.config.Namer, explicit, data.SQL, data.Args)
	name := sqltrace.NameFromContext(ctx)
	if name == "" {
		name = meta.Name
	}
	attrs := meta.Attributes(t.config.System, t.config.Namespace)
	attrs = append(attrs, sqltrace.QuerySummaryKey.String(name))
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return context.WithValue(ctx, spanKey{}, span)
}

func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, _ := ctx.Value(spanKey{}).(trace.Span)
	gotel.End(span, data.Err)
}

type copySpanKey struct{}

func (t *Tracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	name := t.config.Namer.Name("CopyFrom")
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		sqltrace.SystemKey.String(t.config.System), sqltrace.LegacySystemKey.String(t.config.System), sqltrace.NamespaceKey.String(t.config.Namespace),
		sqltrace.OperationKey.String("COPY"), sqltrace.QuerySummaryKey.String(name),
		attribute.Int("db.query.column.count", len(data.ColumnNames)),
	))
	return context.WithValue(ctx, copySpanKey{}, span)
}

func (t *Tracer) TraceCopyFromEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceCopyFromEndData) {
	span, _ := ctx.Value(copySpanKey{}).(trace.Span)
	gotel.End(span, data.Err)
}

var _ pgx.QueryTracer = (*Tracer)(nil)
var _ pgx.CopyFromTracer = (*Tracer)(nil)
