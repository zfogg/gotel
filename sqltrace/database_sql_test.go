package sqltrace

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestDatabaseSQLHelpersCompleteSpansAndCountRows(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	config := SpanConfig{Tracer: otel.Tracer("test"), Namer: MustNamer("test.storage"), Dialect: DialectSQLite, System: "sqlite", Namespace: "test"}
	if _, err := ExecContext(t.Context(), db, config, "CREATE TABLE widgets(id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecContext(t.Context(), db, config, "INSERT INTO widgets(name) VALUES(?), (?)", "secret-one", "secret-two"); err != nil {
		t.Fatal(err)
	}
	rows, err := QueryContext(t.Context(), db, config, "SELECT name FROM widgets WHERE id IN (?, ?) ORDER BY id", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	row := QueryRowContext(t.Context(), db, config, "SELECT name FROM widgets WHERE id = ?", 1)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || name != "secret-one" {
		t.Fatalf("names = %#v, name = %q", names, name)
	}
	spans := recorder.Ended()
	if len(spans) != 4 {
		t.Fatalf("ended spans = %d", len(spans))
	}
	if got := int64Attribute(spans[2].Attributes(), "db.response.returned_rows"); got != 2 {
		t.Fatalf("query rows = %d", got)
	}
	if got := int64Attribute(spans[3].Attributes(), "db.response.returned_rows"); got != 1 {
		t.Fatalf("query-row rows = %d", got)
	}
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			if attr.Key == QueryTextKey && (attr.Value.AsString() == "secret-one" || attr.Value.AsString() == "secret-two") {
				t.Fatalf("span retained bind value: %q", attr.Value.AsString())
			}
		}
	}
}

func int64Attribute(attrs []attribute.KeyValue, key string) int64 {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsInt64()
		}
	}
	return -1
}
