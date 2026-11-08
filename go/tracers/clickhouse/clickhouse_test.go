package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	chproto "github.com/ClickHouse/ch-go/proto"
	native "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/column"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	chblock "github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	gotelsql "github.com/zfogg/gotel/go/tracers/sql"
	sequel "github.com/zfogg/sequel/go"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type nativeConn struct {
	driver.Conn
	err        error
	calls      int
	ctx        context.Context
	query      string
	args       []any
	dest       any
	rows       driver.Rows
	row        driver.Row
	batch      driver.Batch
	data       io.Reader
	stream     io.ReadCloser
	panicValue any
}

func (n *nativeConn) record(ctx context.Context, query string, args []any) {
	n.calls++
	n.ctx = ctx
	n.query = query
	n.args = args
	if n.panicValue != nil {
		panic(n.panicValue)
	}
}

func TestNativePanicIdentityAndSpanCleanup(t *testing.T) {
	original := errors.New("PRIVATE_PANIC")
	n := &nativeConn{panicValue: original}
	conn, sr := fixture(t, n, nil)
	func() {
		defer func() {
			if recover() != original {
				t.Fatal("panic identity changed")
			}
		}()
		_ = conn.Exec(context.Background(), "SELECT 1")
	}()
	if len(sr.Ended()) != 1 || attrs(sr.Ended()[0])["operation.outcome"].AsString() != "error" {
		t.Fatal("panic span not finished")
	}
	private(t, sr)
}
func (n *nativeConn) Exec(ctx context.Context, q string, args ...any) error {
	n.record(ctx, q, args)
	return n.err
}
func (n *nativeConn) Select(ctx context.Context, d any, q string, args ...any) error {
	n.dest = d
	n.record(ctx, q, args)
	return n.err
}
func (n *nativeConn) Query(ctx context.Context, q string, args ...any) (driver.Rows, error) {
	n.record(ctx, q, args)
	return n.rows, n.err
}
func (n *nativeConn) QueryRow(ctx context.Context, q string, args ...any) driver.Row {
	n.record(ctx, q, args)
	return n.row
}
func (n *nativeConn) PrepareBatch(ctx context.Context, q string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
	n.record(ctx, q, nil)
	return n.batch, n.err
}
func (n *nativeConn) AsyncInsert(ctx context.Context, q string, _ bool, args ...any) error {
	n.record(ctx, q, args)
	return n.err
}
func (n *nativeConn) QueryFormat(ctx context.Context, _ string, q string, args ...any) (io.ReadCloser, error) {
	n.record(ctx, q, args)
	return n.stream, n.err
}
func (n *nativeConn) InsertFormat(ctx context.Context, _ string, q string, r io.Reader) error {
	n.record(ctx, q, nil)
	n.data = r
	return n.err
}

type nativeRows struct {
	driver.Rows
	remaining     int
	err, closeErr error
	closeCalls    int
	dest          any
}

func (r *nativeRows) Next() bool {
	if r.remaining > 0 {
		r.remaining--
		return true
	}
	return false
}
func (r *nativeRows) Scan(dest ...any) error    { r.dest = dest[0]; return r.err }
func (r *nativeRows) ScanStruct(dest any) error { r.dest = dest; return r.err }
func (r *nativeRows) Totals(dest ...any) error  { r.dest = dest[0]; return r.err }
func (r *nativeRows) Err() error                { return r.err }
func (r *nativeRows) Close() error              { r.closeCalls++; return r.closeErr }

type nativeRow struct {
	driver.Row
	err  error
	dest any
}

func (r *nativeRow) Scan(dest ...any) error    { r.dest = dest[0]; return r.err }
func (r *nativeRow) ScanStruct(dest any) error { r.dest = dest; return r.err }
func (r *nativeRow) Err() error                { return r.err }

type nativeBatch struct {
	driver.Batch
	err    error
	sent   bool
	rows   int
	value  any
	column *nativeColumn
}

func (b *nativeBatch) Append(v ...any) error         { b.value = v[0]; b.rows++; return b.err }
func (b *nativeBatch) AppendStruct(v any) error      { b.value = v; b.rows++; return b.err }
func (b *nativeBatch) Column(int) driver.BatchColumn { return b.column }
func (b *nativeBatch) Flush() error                  { return b.err }
func (b *nativeBatch) Send() error                   { b.sent = true; return b.err }
func (b *nativeBatch) Abort() error                  { b.sent = true; return b.err }
func (b *nativeBatch) Close() error                  { b.sent = true; return b.err }
func (b *nativeBatch) IsSent() bool                  { return b.sent }
func (b *nativeBatch) Rows() int                     { return b.rows }

type nativeColumn struct {
	driver.BatchColumn
	value any
	err   error
}

func (c *nativeColumn) Append(v any) error    { c.value = v; return c.err }
func (c *nativeColumn) AppendRow(v any) error { c.value = v; return c.err }

func fixture(t *testing.T, n *nativeConn, policy *sequel.AnalysisOptions) (*Conn, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return Wrap(n, Config{Tracer: provider.Tracer("test"), Namespace: "analytics", Analysis: policy}), sr
}
func attrs(s sdktrace.ReadOnlySpan) map[string]attribute.Value {
	out := map[string]attribute.Value{}
	for _, kv := range s.Attributes() {
		out[string(kv.Key)] = kv.Value
	}
	return out
}
func private(t *testing.T, sr *tracetest.SpanRecorder) {
	t.Helper()
	for _, s := range sr.Ended() {
		if strings.Contains(fmt.Sprint(s.Attributes()), "PRIVATE") {
			t.Fatalf("private data in span %s", s.Name())
		}
	}
}

func TestNativeCallsResultsPrivacyAndDiagnostics(t *testing.T) {
	n := &nativeConn{}
	conn, sr := fixture(t, n, nil)
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	type key struct{}
	ctx := context.WithValue(trace.ContextWithSpanContext(context.Background(), parent), key{}, "original")
	argument := &struct{ secret string }{"PRIVATE_BIND"}
	query := "SELECT {id:UInt64}, 'PRIVATE_LITERAL'"
	if err := conn.Exec(ctx, query, argument); err != nil {
		t.Fatal(err)
	}
	if n.query != query || n.args[0] != argument || n.calls != 1 || n.ctx.Value(key{}) != "original" {
		t.Fatal("native call changed")
	}
	if !trace.SpanContextFromContext(n.ctx).IsValid() {
		t.Fatal("missing child context")
	}
	span := sr.Ended()[0]
	if span.Parent().SpanID() != parent.SpanID() {
		t.Fatal("wrong parent")
	}
	a := attrs(span)
	if a["db.system.name"].AsString() != "clickhouse" || !strings.HasPrefix(a["gotel.db.query.fingerprint"].AsString(), "sequel:v1:clickhouse:") || len(a["db.query.diagnostics"].AsStringSlice()) != 0 {
		t.Fatal(a)
	}
	sentinel := &native.Exception{Code: 62, Message: "PRIVATE_ERROR"}
	n.err = sentinel
	if err := conn.Exec(ctx, "SELECT 'PRIVATE_UNCLOSED"); err != sentinel {
		t.Fatal("error identity changed")
	}
	a = attrs(sr.Ended()[1])
	if _, ok := a["db.query.text"]; ok {
		t.Fatal("malformed text exported")
	}
	if !strings.Contains(a["db.query.diagnostics"].AsStringSlice()[0], "unterminated_string") || a["db.response.status_code"].AsInt64() != 62 {
		t.Fatal(a)
	}
	private(t, sr)
}

func TestRowsAndSingleRowsFinishOncePreserveCloseResult(t *testing.T) {
	n := &nativeConn{rows: &nativeRows{remaining: 2}, row: &nativeRow{}}
	conn, sr := fixture(t, n, nil)
	ctx := context.Background()
	rows, err := conn.Query(ctx, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Ended()) != 0 {
		t.Fatal("cursor ended early")
	}
	var value int
	for rows.Next() {
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(sr.Ended()) != 1 || attrs(sr.Ended()[0])["db.response.returned_rows"].AsInt64() != 2 {
		t.Fatal("wrong row lifetime")
	}
	row := conn.QueryRow(ctx, "SELECT 1")
	if len(sr.Ended()) != 1 {
		t.Fatal("row ended early")
	}
	if err := row.Scan(&value); err != nil {
		t.Fatal(err)
	}
	if n.row.(*nativeRow).dest != &value {
		t.Fatal("destination changed")
	}
	late := errors.New("PRIVATE_LATE")
	n.rows = &nativeRows{err: late}
	rows, _ = conn.Query(ctx, "SELECT 1")
	if err := rows.Close(); err != nil {
		t.Fatal("native nil Close result replaced")
	}
	if attrs(sr.Ended()[2])["operation.outcome"].AsString() != "error" {
		t.Fatal("late error omitted")
	}
	private(t, sr)
}

func TestBatchesPayloadsAbortAndFormatStreams(t *testing.T) {
	native := &nativeBatch{column: &nativeColumn{}}
	n := &nativeConn{batch: native, stream: io.NopCloser(strings.NewReader("PRIVATE_RESPONSE"))}
	conn, sr := fixture(t, n, nil)
	ctx := context.Background()
	batch, err := conn.PrepareBatch(ctx, "INSERT INTO events")
	if err != nil {
		t.Fatal(err)
	}
	value := &struct{ secret string }{"PRIVATE_ROW"}
	if err := batch.Append(value); err != nil {
		t.Fatal(err)
	}
	if native.value != value {
		t.Fatal("row replaced")
	}
	if err := batch.Column(0).Append(value); err != nil {
		t.Fatal(err)
	}
	if native.column.value != value {
		t.Fatal("column replaced")
	}
	if err := batch.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(sr.Ended()) != 0 {
		t.Fatal("flush ended insert")
	}
	if err := batch.Send(); err != nil {
		t.Fatal(err)
	}
	_ = batch.Close()
	if len(sr.Ended()) != 1 {
		t.Fatal("insert ended twice")
	}
	n.batch = &nativeBatch{}
	batch, _ = conn.PrepareBatch(ctx, "INSERT INTO events")
	_ = batch.Abort()
	if attrs(sr.Ended()[1])["operation.outcome"].AsString() != "cancelled" {
		t.Fatal("abort not cancelled")
	}
	stream, err := conn.QueryFormat(ctx, "CSV", "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(stream)
	if err != nil || string(data) != "PRIVATE_RESPONSE" {
		t.Fatal("stream changed")
	}
	_ = stream.Close()
	input := strings.NewReader("PRIVATE_INSERT_PAYLOAD")
	if err := conn.InsertFormat(ctx, "CSV", "INSERT INTO events", input); err != nil {
		t.Fatal(err)
	}
	if n.data != input {
		t.Fatal("payload reader replaced")
	}
	private(t, sr)
}

func TestCollectionPoliciesFailOpen(t *testing.T) {
	for _, policy := range []sequel.AnalysisOptions{{Dialect: gotelsql.DialectSQLite, OmitQueryText: true}, {MaxWorkUnits: 1}} {
		n := &nativeConn{}
		conn, sr := fixture(t, n, &policy)
		if err := conn.Exec(context.Background(), "SELECT 'PRIVATE_LITERAL'"); err != nil || n.calls != 1 {
			t.Fatal("analysis blocked driver")
		}
		a := attrs(sr.Ended()[0])
		if _, ok := a["db.query.text"]; ok {
			t.Fatal("omitted text present")
		}
		if a["operation.outcome"].AsString() != "success" {
			t.Fatal(a)
		}
		private(t, sr)
	}
}

func TestOfficialHTTPClientPreservesQueryOptionsAndPropagation(t *testing.T) {
	block := chblock.NewBlock()
	for _, spec := range []struct {
		name string
		kind column.Type
	}{{"displayName()", "String"}, {"version()", "String"}, {"revision()", "UInt32"}, {"timezone()", "String"}} {
		if err := block.AddColumn(spec.name, spec.kind); err != nil {
			t.Fatal(err)
		}
	}
	if err := block.Append("test", "26.1.1.1", uint32(native.ClientTCPProtocolVersion), "UTC"); err != nil {
		t.Fatal(err)
	}
	var buffer chproto.Buffer
	if err := block.Encode(&buffer, native.ClientTCPProtocolVersion); err != nil {
		t.Fatal(err)
	}
	type request struct {
		query  string
		values url.Values
		parent string
	}
	received := make(chan request, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		query := r.URL.Query().Get("query") + string(body)
		received <- request{query: query, values: r.URL.Query(), parent: r.Header.Get("traceparent")}
		w.Header().Set("X-ClickHouse-Server-Display-Name", "test")
		if strings.Contains(query, "version()") {
			_, _ = w.Write(buffer.Buf)
			return
		}
		_, _ = io.WriteString(w, "")
	}))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())
	transportCalled := false
	conn, err := Open(&native.Options{Addr: []string{address}, Protocol: native.HTTP, Auth: native.Auth{Database: "analytics"}, TransportFunc: func(base *http.Transport) (http.RoundTripper, error) { transportCalled = true; return base, nil }}, Config{Tracer: provider.Tracer("native-http"), Namespace: "analytics"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := native.Context(context.Background(), native.WithQueryID("original-id"), native.WithSettings(native.Settings{"max_threads": 1}))
	if err := conn.Exec(ctx, "SELECT ?", "PRIVATE_BIND"); err != nil {
		t.Fatal(err)
	}
	close(received)
	var actual request
	for req := range received {
		if strings.Contains(req.query, "PRIVATE_BIND") {
			actual = req
		}
	}
	if actual.query == "" || actual.values.Get("query_id") != "original-id" || actual.values.Get("max_threads") != "1" || actual.parent == "" {
		t.Fatalf("native request options changed: %#v", actual)
	}
	if !strings.Contains(actual.parent, recorder.Ended()[0].SpanContext().SpanID().String()) {
		t.Fatal("wrong propagated child")
	}
	if !transportCalled {
		t.Fatal("native TransportFunc skipped")
	}
	private(t, recorder)
}
