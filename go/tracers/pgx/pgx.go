// Package pgx instruments pgx with stable names and safe SQL templates.
package pgx

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zfogg/gotel/go/outcome"
	gotelsql "github.com/zfogg/gotel/go/tracers/sql"
	sequel "github.com/zfogg/sequel/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config controls pgx database span names and attributes.
type Config struct {
	Scope     string                                     // OpenTelemetry instrumentation scope.
	Namer     gotelsql.Namer                             // Prefix and validation for database span names.
	System    string                                     // db.system.name; defaults to postgresql.
	Namespace string                                     // Logical database name recorded as db.namespace.
	Sanitize  gotelsql.SanitizeOptions                   // SQL template policy; defaults to PostgreSQL.
	Analysis  *sequel.AnalysisOptions                    // Optional unified budgets and bind descriptors.
	Observe   gotelsql.Observer                          // Optional low-cardinality duration observer.
	Outcome   func(context.Context, error) outcome.Value // Optional explicit completion policy; errors returned unchanged.
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
	if config.Namer.Prefix == "" {
		config.Namer = gotelsql.StorageNamer(config.System)
	}
	if config.Sanitize.Dialect == "" {
		config.Sanitize = sequel.DefaultSanitizeOptions(gotelsql.DialectPostgreSQL)
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

func (t *Tracer) inspect(ctx context.Context, name, statement string, args []any) gotelsql.Metadata {
	if t.config.Analysis != nil {
		options := *t.config.Analysis
		if options.Context == nil {
			options.Context = ctx
		}
		if options.Dialect == "" {
			options.Dialect = gotelsql.DialectPostgreSQL
		}
		return gotelsql.InspectAnalysis(t.config.Namer, name, statement, args, options)
	}
	return gotelsql.InspectWithOptions(t.config.Namer, name, statement, args, t.config.Sanitize)
}

// TraceQueryStart starts a named query span.
func (t *Tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if owned, _ := ctx.Value(lifecycleQueryKey{}).(bool); owned || gotelsql.TracingDisabled(ctx) {
		return ctx
	}
	meta := t.inspect(ctx, "", data.SQL, data.Args)
	name := gotelsql.NameFromContext(ctx)
	if name == "" {
		name = meta.Name
	}
	attrs := meta.Attributes(t.config.System, t.config.Namespace)
	attrs = append(attrs, connectionAttributes(conn)...)
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return context.WithValue(ctx, querySpanKey{}, spanState{span, time.Now(), meta.Operation})
}

// TraceQueryEnd records result metadata and ends a query span.
func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if owned, _ := ctx.Value(lifecycleQueryKey{}).(bool); owned || gotelsql.TracingDisabled(ctx) {
		return
	}
	t.end(ctx, stateFrom(ctx, querySpanKey{}), data.Err, data.CommandTag.RowsAffected())
}

// TraceCopyFromStart starts a COPY span.
func (t *Tracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	if gotelsql.TracingDisabled(ctx) {
		return ctx
	}
	name := t.config.Namer.MustName("CopyFrom")
	meta := t.inspect(ctx, "CopyFrom", "COPY "+data.TableName.Sanitize()+" FROM STDIN", nil)
	attrs := append(meta.Attributes(t.config.System, t.config.Namespace), attribute.Int("db.query.column.count", len(data.ColumnNames)))
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
	if gotelsql.TracingDisabled(ctx) {
		return ctx
	}
	name := t.config.Namer.MustName("Batch")
	attrs := []attribute.KeyValue{gotelsql.SystemKey.String(t.config.System), gotelsql.NamespaceKey.String(t.config.Namespace), gotelsql.OperationKey.String("BATCH")}
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
	meta := t.inspect(ctx, "", data.SQL, data.Args)
	attrs := append(meta.Attributes(t.config.System, t.config.Namespace), resultAttributes(data.Err, data.CommandTag.RowsAffected())...)
	st.span.AddEvent("db.batch.query", trace.WithAttributes(attrs...))
}

// TraceBatchEnd records and ends the batch span.
func (t *Tracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchEndData) {
	t.end(ctx, stateFrom(ctx, batchSpanKey{}), data.Err, -1)
}

// TracePrepareStart starts a statement preparation span.
func (t *Tracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	if gotelsql.TracingDisabled(ctx) {
		return ctx
	}
	meta := t.inspect(ctx, "Prepare", data.SQL, nil)
	attrs := append(meta.Attributes(t.config.System, t.config.Namespace), attribute.String("db.stored_procedure.name", data.Name))
	attrs = append(attrs, gotelsql.OperationKey.String("PREPARE"))
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
	if gotelsql.TracingDisabled(ctx) {
		return ctx
	}
	name := t.config.Namer.MustName("Connect")
	attrs := []attribute.KeyValue{gotelsql.SystemKey.String(t.config.System), gotelsql.NamespaceKey.String(t.config.Namespace), gotelsql.OperationKey.String("CONNECT")}
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
	classify := t.config.Outcome
	if classify == nil {
		classify = outcome.Classify
	}
	result := classify(ctx, err)
	telemetryErr := err
	if result != outcome.Error {
		telemetryErr = nil
	}
	st.span.SetAttributes(resultAttributes(telemetryErr, rows)...)
	outcome.Apply(st.span, result, err)
	st.span.End()
	if t.config.Observe != nil {
		label := string(result)
		if result == outcome.Success {
			label = "ok"
		} // Preserve the existing observer contract.
		t.config.Observe(ctx, st.operation, label, time.Since(st.started))
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
