// Package pgxtrace instruments pgx with stable names and safe SQL templates.
package pgxtrace

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zfogg/gotel"
	"github.com/zfogg/gotel/sqltrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config controls pgx database span names and attributes.
type Config struct {
	Scope     string                   // OpenTelemetry instrumentation scope.
	Namer     sqltrace.Namer           // Prefix and validation for database span names.
	System    string                   // db.system.name; defaults to postgresql.
	Namespace string                   // Logical database name recorded as db.namespace.
	Sanitize  sqltrace.SanitizeOptions // SQL template policy; defaults to PostgreSQL.
	Observe   sqltrace.Observer        // Optional low-cardinality duration observer.
}

// Tracer implements every pgx tracing hook with safe metadata.
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
	if config.Sanitize.Dialect == "" {
		config.Sanitize = sqltrace.DefaultSanitizeOptions(sqltrace.DialectPostgreSQL)
	}
	return &Tracer{config: config, tracer: otel.Tracer(config.Scope)}
}

type spanState struct {
	span      trace.Span
	started   time.Time
	operation string
}
type querySpanKey struct{}
type copySpanKey struct{}
type batchSpanKey struct{}
type prepareSpanKey struct{}
type connectSpanKey struct{}

// TraceQueryStart starts a named query span.
func (t *Tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if sqltrace.TracingDisabled(ctx) {
		return ctx
	}
	meta := sqltrace.InspectWithOptions(t.config.Namer, "", data.SQL, data.Args, t.config.Sanitize)
	name := sqltrace.NameFromContext(ctx)
	if name == "" {
		name = meta.Name
	}
	attrs := append(meta.Attributes(t.config.System, t.config.Namespace), sqltrace.QuerySummaryKey.String(name))
	attrs = append(attrs, connectionAttributes(conn)...)
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return context.WithValue(ctx, querySpanKey{}, spanState{span, time.Now(), meta.Operation})
}

// TraceQueryEnd records result metadata and ends a query span.
func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	t.end(ctx, stateFrom(ctx, querySpanKey{}), data.Err, data.CommandTag.RowsAffected())
}

// TraceCopyFromStart starts a COPY span.
func (t *Tracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	if sqltrace.TracingDisabled(ctx) {
		return ctx
	}
	name := t.config.Namer.MustName("CopyFrom")
	attrs := []attribute.KeyValue{sqltrace.SystemKey.String(t.config.System), sqltrace.LegacySystemKey.String(t.config.System), sqltrace.NamespaceKey.String(t.config.Namespace), sqltrace.OperationKey.String("COPY"), sqltrace.QuerySummaryKey.String(name), attribute.Int("db.query.column.count", len(data.ColumnNames))}
	attrs = append(attrs, connectionAttributes(conn)...)
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return context.WithValue(ctx, copySpanKey{}, spanState{span, time.Now(), "COPY"})
}

// TraceCopyFromEnd records and ends a COPY span.
func (t *Tracer) TraceCopyFromEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceCopyFromEndData) {
	t.end(ctx, stateFrom(ctx, copySpanKey{}), data.Err, data.CommandTag.RowsAffected())
}

// TraceBatchStart starts one span for a pgx batch.
func (t *Tracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	if sqltrace.TracingDisabled(ctx) {
		return ctx
	}
	name := t.config.Namer.MustName("Batch")
	attrs := []attribute.KeyValue{sqltrace.SystemKey.String(t.config.System), sqltrace.LegacySystemKey.String(t.config.System), sqltrace.NamespaceKey.String(t.config.Namespace), sqltrace.QuerySummaryKey.String(name)}
	if data.Batch != nil {
		attrs = append(attrs, attribute.Int("db.operation.batch.size", data.Batch.Len()))
	}
	attrs = append(attrs, connectionAttributes(conn)...)
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return context.WithValue(ctx, batchSpanKey{}, spanState{span, time.Now(), "BATCH"})
}

// TraceBatchQuery adds safe per-query metadata to the batch span.
func (t *Tracer) TraceBatchQuery(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	st := stateFrom(ctx, batchSpanKey{})
	if st.span == nil {
		return
	}
	meta := sqltrace.InspectWithOptions(t.config.Namer, "", data.SQL, data.Args, t.config.Sanitize)
	attrs := append(meta.Attributes(t.config.System, t.config.Namespace), resultAttributes(data.Err, data.CommandTag.RowsAffected())...)
	st.span.AddEvent("db.batch.query", trace.WithAttributes(attrs...))
}

// TraceBatchEnd records and ends the batch span.
func (t *Tracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchEndData) {
	t.end(ctx, stateFrom(ctx, batchSpanKey{}), data.Err, -1)
}

// TracePrepareStart starts a statement preparation span.
func (t *Tracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	if sqltrace.TracingDisabled(ctx) {
		return ctx
	}
	meta := sqltrace.InspectWithOptions(t.config.Namer, "Prepare", data.SQL, nil, t.config.Sanitize)
	attrs := append(meta.Attributes(t.config.System, t.config.Namespace), attribute.String("db.stored_procedure.name", data.Name))
	attrs = append(attrs, connectionAttributes(conn)...)
	ctx, span := t.tracer.Start(ctx, meta.Name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return context.WithValue(ctx, prepareSpanKey{}, spanState{span, time.Now(), "PREPARE"})
}

// TracePrepareEnd records reuse and ends the preparation span.
func (t *Tracer) TracePrepareEnd(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareEndData) {
	st := stateFrom(ctx, prepareSpanKey{})
	if st.span != nil {
		st.span.SetAttributes(attribute.Bool("db.operation.already_prepared", data.AlreadyPrepared))
	}
	t.end(ctx, st, data.Err, -1)
}

// TraceConnectStart starts a connection span.
func (t *Tracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	if sqltrace.TracingDisabled(ctx) {
		return ctx
	}
	name := t.config.Namer.MustName("Connect")
	attrs := []attribute.KeyValue{sqltrace.SystemKey.String(t.config.System), sqltrace.LegacySystemKey.String(t.config.System), sqltrace.NamespaceKey.String(t.config.Namespace), sqltrace.QuerySummaryKey.String(name)}
	if data.ConnConfig != nil {
		attrs = append(attrs, attribute.String("server.address", data.ConnConfig.Host), attribute.Int("server.port", int(data.ConnConfig.Port)))
	}
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return context.WithValue(ctx, connectSpanKey{}, spanState{span, time.Now(), "CONNECT"})
}

// TraceConnectEnd records and ends a connection span.
func (t *Tracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	t.end(ctx, stateFrom(ctx, connectSpanKey{}), data.Err, -1)
}

func stateFrom(ctx context.Context, key any) spanState {
	st, _ := ctx.Value(key).(spanState)
	return st
}
func (t *Tracer) end(ctx context.Context, st spanState, err error, rows int64) {
	if st.span == nil {
		return
	}
	st.span.SetAttributes(resultAttributes(err, rows)...)
	gotel.End(st.span, err)
	if t.config.Observe != nil {
		result := "ok"
		if err != nil {
			result = "error"
		}
		t.config.Observe(ctx, st.operation, result, time.Since(st.started))
	}
}
func resultAttributes(err error, rows int64) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)
	if rows >= 0 {
		attrs = append(attrs, attribute.Int64("db.response.returned_rows", rows))
	}
	if err != nil {
		attrs = append(attrs, attribute.String("error.type", fmt.Sprintf("%T", err)))
		if pgErr, ok := err.(*pgconn.PgError); ok {
			attrs = append(attrs, attribute.String("db.response.status_code", pgErr.Code))
		}
	}
	return attrs
}
func connectionAttributes(conn *pgx.Conn) []attribute.KeyValue {
	if conn == nil {
		return nil
	}
	config := conn.Config()
	return []attribute.KeyValue{attribute.String("server.address", config.Host), attribute.Int("server.port", int(config.Port))}
}

var _ pgx.QueryTracer = (*Tracer)(nil)
var _ pgx.CopyFromTracer = (*Tracer)(nil)
var _ pgx.BatchTracer = (*Tracer)(nil)
var _ pgx.PrepareTracer = (*Tracer)(nil)
var _ pgx.ConnectTracer = (*Tracer)(nil)
