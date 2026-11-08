// Package clickhouse instruments the official clickhouse-go/v2 native API.
// It preserves driver arguments/results and observes rows, formatted streams and
// batch inserts through completion. SQL values and exception messages are omitted.
package clickhouse

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	native "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	gotelsql "github.com/zfogg/gotel/go/tracers/sql"
	sequel "github.com/zfogg/sequel/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Config selects owned providers, database identity, SQL policies and metrics.
type Config struct {
	Tracer    trace.Tracer            // Nil selects the global gotel/clickhouse tracer.
	Namer     gotelsql.Namer          // Defaults to storage.clickhouse.
	Namespace string                  // Stable database name, without credentials.
	Analysis  *sequel.AnalysisOptions // Optional policy; the actual dialect is forced.
	Observe   gotelsql.Observer       // Optional low-cardinality duration observer.
}

// Conn decorates a native connection. Unoverridden driver extensions are forwarded
// directly; use the documented query, stream and batch methods for tracing.
type Conn struct {
	driver.Conn
	config Config
}

// Wrap keeps the existing client's transports, credentials, timeouts and pools.
func Wrap(conn driver.Conn, config Config) *Conn {
	if config.Tracer == nil {
		config.Tracer = otel.Tracer("gotel/clickhouse")
	}
	if config.Namer.Prefix == "" {
		config.Namer = gotelsql.StorageNamer("clickhouse")
	}
	return &Conn{Conn: conn, config: config}
}

// Open opens the official native client and installs per-request W3C propagation
// for HTTP as well as native-protocol WithSpan propagation. The caller's options
// and existing TransportFunc are retained; shared header maps are not modified.
func Open(options *native.Options, config Config) (*Conn, error) {
	var copy native.Options
	if options != nil {
		copy = *options
	}
	previous := copy.TransportFunc
	copy.TransportFunc = func(base *http.Transport) (http.RoundTripper, error) {
		var transport http.RoundTripper = base
		if previous != nil {
			var err error
			transport, err = previous(base)
			if err != nil {
				return nil, err
			}
		}
		return HTTPTransport(transport), nil
	}
	conn, err := native.Open(&copy)
	if err != nil {
		return nil, err
	}
	return Wrap(conn, config), nil
}

// HTTPTransport injects the current query's W3C context on a cloned request.
// It creates no extra spans and preserves the native body/response and errors.
func HTTPTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &httpTransport{base: base}
}

type httpTransport struct{ base http.RoundTripper }

func (t *httpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	propagation.TraceContext{}.Inject(copy.Context(), propagation.HeaderCarrier(copy.Header))
	return t.base.RoundTrip(copy)
}

func (c *Conn) start(ctx context.Context, query string, args []any) (context.Context, *gotelsql.DBSpan) {
	options := sequel.AnalysisOptions{Dialect: gotelsql.DialectClickHouse, Format: true, OmitSourceMaps: true, OmitBindDescriptors: true}
	if c.config.Analysis != nil {
		options = *c.config.Analysis
		options.Dialect = gotelsql.DialectClickHouse
	}
	ctx, span := gotelsql.StartSpan(ctx, gotelsql.SpanConfig{Tracer: c.config.Tracer, Namer: c.config.Namer, Statement: query, Args: args, Dialect: gotelsql.DialectClickHouse, Analysis: &options, System: "clickhouse", Namespace: c.config.Namespace, Observe: c.config.Observe})
	if parent := trace.SpanContextFromContext(ctx); parent.IsValid() {
		ctx = native.Context(ctx, native.WithSpan(parent))
	}
	return ctx, span
}

// Exec executes the original query once and returns its original error.
func (c *Conn) Exec(ctx context.Context, query string, args ...any) error {
	ctx, span := c.start(ctx, query, args)
	defer endOnPanic(ctx, span)
	err := c.Conn.Exec(ctx, query, args...)
	finish(ctx, span, err, -1)
	return err
}

// Select forwards the original destination and completes after row decoding.
func (c *Conn) Select(ctx context.Context, dest any, query string, args ...any) error {
	ctx, span := c.start(ctx, query, args)
	defer endOnPanic(ctx, span)
	err := c.Conn.Select(ctx, dest, query, args...)
	finish(ctx, span, err, -1)
	return err
}

// Query traces the complete result cursor. Call Close when stopping before EOF.
func (c *Conn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	ctx, span := c.start(ctx, query, args)
	defer endOnPanic(ctx, span)
	rows, err := c.Conn.Query(ctx, query, args...)
	if err != nil {
		finish(ctx, span, err, -1)
		return rows, err
	}
	return &Rows{Rows: rows, ctx: ctx, span: span}, nil
}

// QueryRow traces until Scan/ScanStruct, or an Err call reporting a failure.
func (c *Conn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	ctx, span := c.start(ctx, query, args)
	defer endOnPanic(ctx, span)
	return &Row{Row: c.Conn.QueryRow(ctx, query, args...), ctx: ctx, span: span}
}

// AsyncInsert preserves native asynchronous acknowledgement/wait behavior.
func (c *Conn) AsyncInsert(ctx context.Context, query string, wait bool, args ...any) error {
	ctx, span := c.start(ctx, query, args)
	defer endOnPanic(ctx, span)
	err := c.Conn.AsyncInsert(ctx, query, wait, args...)
	finish(ctx, span, err, -1)
	return err
}

// PrepareBatch traces preparation, buffering and final Send/Abort/Close.
func (c *Conn) PrepareBatch(ctx context.Context, query string, opts ...driver.PrepareBatchOption) (driver.Batch, error) {
	ctx, span := c.start(ctx, query, nil)
	defer endOnPanic(ctx, span)
	batch, err := c.Conn.PrepareBatch(ctx, query, opts...)
	if err != nil {
		finish(ctx, span, err, -1)
		return batch, err
	}
	return &Batch{Batch: batch, ctx: ctx, span: span}, nil
}

// QueryFormat preserves native format bytes and late streaming errors.
func (c *Conn) QueryFormat(ctx context.Context, format, query string, args ...any) (io.ReadCloser, error) {
	ctx, span := c.start(ctx, query, args)
	defer endOnPanic(ctx, span)
	stream, err := c.Conn.QueryFormat(ctx, format, query, args...)
	if err != nil {
		finish(ctx, span, err, -1)
		return stream, err
	}
	return &Stream{ReadCloser: stream, ctx: ctx, span: span}, nil
}

// InsertFormat forwards the original reader, without inspecting its payload.
func (c *Conn) InsertFormat(ctx context.Context, format, query string, data io.Reader) error {
	ctx, span := c.start(ctx, query, nil)
	defer endOnPanic(ctx, span)
	err := c.Conn.InsertFormat(ctx, format, query, data)
	finish(ctx, span, err, -1)
	return err
}

// Rows embeds native metadata methods and completes once on EOF, error or Close.
type Rows struct {
	driver.Rows
	ctx   context.Context
	span  *gotelsql.DBSpan
	count int64
}

// Next preserves advancement and records the number of returned rows at EOF.
func (r *Rows) Next() bool {
	if r.Rows.Next() {
		r.count++
		return true
	}
	finish(r.ctx, r.span, r.Rows.Err(), r.count)
	return false
}

// Scan returns the native scan error, without recording its message.
func (r *Rows) Scan(dest ...any) error {
	err := r.Rows.Scan(dest...)
	if err != nil {
		finish(r.ctx, r.span, err, r.count)
	}
	return err
}

// ScanStruct preserves native struct decoding.
func (r *Rows) ScanStruct(dest any) error {
	err := r.Rows.ScanStruct(dest)
	if err != nil {
		finish(r.ctx, r.span, err, r.count)
	}
	return err
}

// Totals preserves the native totals row and any error.
func (r *Rows) Totals(dest ...any) error {
	err := r.Rows.Totals(dest...)
	if err != nil {
		finish(r.ctx, r.span, err, r.count)
	}
	return err
}

// Err observes late errors without ending successful cursors prematurely.
func (r *Rows) Err() error {
	err := r.Rows.Err()
	if err != nil {
		finish(r.ctx, r.span, err, r.count)
	}
	return err
}

// Close releases the native cursor and completes telemetry exactly once.
func (r *Rows) Close() error {
	err := r.Rows.Close()
	observed := err
	if observed == nil {
		observed = r.Rows.Err()
	}
	finish(r.ctx, r.span, observed, r.count)
	return err
}

// Row embeds the native single-row object; the caller must scan it.
type Row struct {
	driver.Row
	ctx  context.Context
	span *gotelsql.DBSpan
}

// Scan completes native single-row decoding.
func (r *Row) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	finish(r.ctx, r.span, err, -1)
	return err
}

// ScanStruct completes native single-row struct decoding.
func (r *Row) ScanStruct(dest any) error {
	err := r.Row.ScanStruct(dest)
	finish(r.ctx, r.span, err, -1)
	return err
}

// Err observes a native single-row failure.
func (r *Row) Err() error {
	err := r.Row.Err()
	if err != nil {
		finish(r.ctx, r.span, err, -1)
	}
	return err
}

// Batch forwards original row/column payloads and owns one insertion observation.
type Batch struct {
	driver.Batch
	ctx  context.Context
	span *gotelsql.DBSpan
	once sync.Once
}

func (b *Batch) finish(err error, rows int64) { b.once.Do(func() { finish(b.ctx, b.span, err, rows) }) }

// Append forwards row objects unchanged.
func (b *Batch) Append(values ...any) error {
	err := b.Batch.Append(values...)
	if err != nil {
		b.finish(err, -1)
	}
	return err
}

// AppendStruct forwards the caller's original object.
func (b *Batch) AppendStruct(value any) error {
	err := b.Batch.AppendStruct(value)
	if err != nil {
		b.finish(err, -1)
	}
	return err
}

// Column instruments column append errors without reading values.
func (b *Batch) Column(index int) driver.BatchColumn {
	return &BatchColumn{BatchColumn: b.Batch.Column(index), owner: b}
}

// Flush preserves native incremental batch behavior; success leaves the span open.
func (b *Batch) Flush() error {
	err := b.Batch.Flush()
	if err != nil {
		b.finish(err, -1)
	}
	return err
}

// Send finalizes the original insertion and completes the observation.
func (b *Batch) Send() error {
	count := int64(b.Batch.Rows())
	err := b.Batch.Send()
	b.finish(err, count)
	return err
}

// Abort discards native work, recording cancellation when the abort succeeds.
func (b *Batch) Abort() error {
	err := b.Batch.Abort()
	observed := err
	if observed == nil {
		observed = context.Canceled
	}
	b.finish(observed, -1)
	return err
}

// Close preserves native cleanup and marks unfinished inserts cancelled.
func (b *Batch) Close() error {
	sent := b.Batch.IsSent()
	err := b.Batch.Close()
	observed := err
	if observed == nil && !sent {
		observed = context.Canceled
	}
	b.finish(observed, -1)
	return err
}

// BatchColumn decorates the native column append API.
type BatchColumn struct {
	driver.BatchColumn
	owner *Batch
}

// Append forwards a column payload unchanged.
func (c *BatchColumn) Append(value any) error {
	err := c.BatchColumn.Append(value)
	if err != nil {
		c.owner.finish(err, -1)
	}
	return err
}

// AppendRow forwards a single row value unchanged.
func (c *BatchColumn) AppendRow(value any) error {
	err := c.BatchColumn.AppendRow(value)
	if err != nil {
		c.owner.finish(err, -1)
	}
	return err
}

// Stream observes raw formatted bytes until EOF, native error or Close.
type Stream struct {
	io.ReadCloser
	ctx  context.Context
	span *gotelsql.DBSpan
}

// Read preserves the native count/error pair, including partial bytes on failure.
func (s *Stream) Read(p []byte) (int, error) {
	n, err := s.ReadCloser.Read(p)
	if err != nil {
		observed := err
		if errors.Is(err, io.EOF) {
			observed = nil
		}
		finish(s.ctx, s.span, observed, -1)
	}
	return n, err
}

// Close releases the native stream and finishes telemetry once.
func (s *Stream) Close() error {
	err := s.ReadCloser.Close()
	finish(s.ctx, s.span, err, -1)
	return err
}

var _ driver.Conn = (*Conn)(nil)
var _ driver.Rows = (*Rows)(nil)
var _ driver.Row = (*Row)(nil)
var _ driver.Batch = (*Batch)(nil)

// Finish records numeric native exception codes without exception text.
func finish(ctx context.Context, span *gotelsql.DBSpan, err error, rows int64) {
	var exception *native.Exception
	if errors.As(err, &exception) {
		trace.SpanFromContext(ctx).SetAttributes(attribute.Int("db.response.status_code", int(exception.Code)))
	}
	span.End(ctx, err, rows)
}

var errNativePanic = errors.New("native ClickHouse call panicked")

func endOnPanic(ctx context.Context, span *gotelsql.DBSpan) {
	if original := recover(); original != nil {
		finish(ctx, span, errNativePanic, -1)
		panic(original)
	}
}
