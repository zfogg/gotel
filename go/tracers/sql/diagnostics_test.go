package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	sequel "github.com/zfogg/sequel/go"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOriginalQueryDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		dialect     Dialect
		query, code string
	}{
		{DialectSQLite, "SELECT 'SECRET", "unterminated_string"},
		{DialectPostgreSQL, "SELECT $tag$SECRET", "unterminated_string"},
		{DialectMySQL, "SELECT `SECRET", "unterminated_identifier"},
		{DialectClickHouse, "SELECT {arg:String", "unterminated_parameter"},
		{DialectDuckDB, "SELECT (1", "unclosed_group"},
		{DialectGeneric, "SELECT /* SECRET", "unterminated_comment"},
	} {
		t.Run(string(tc.dialect), func(t *testing.T) {
			options := sequel.DefaultSanitizeOptions(tc.dialect)
			options.OmitQueryText = true
			meta := InspectWithOptions(MustNamer("storage.test"), "Broken", tc.query, nil, options)
			if len(meta.Diagnostics) != 1 || meta.Diagnostics[0].Code != tc.code || meta.SanitizedSQL != "" {
				t.Fatalf("%+v", meta)
			}
			for _, attr := range meta.Attributes(string(tc.dialect), "") {
				if attr.Key != QueryDiagnosticsKey {
					continue
				}
				values := attr.Value.AsStringSlice()
				if len(values) != 1 || strings.Contains(values[0], "SECRET") {
					t.Fatal(values)
				}
				var got sequel.Diagnostic
				if err := json.Unmarshal([]byte(values[0]), &got); err != nil || got.Code != tc.code || got.Line != 1 || got.Column < 1 {
					t.Fatal(got, err)
				}
			}
			meta.Diagnostics[0].Message = "mutated"
			again := InspectWithOptions(MustNamer("storage.test"), "Broken", tc.query, nil, options)
			if again.Diagnostics[0].Message == "mutated" {
				t.Fatal("caller mutated cached diagnostics")
			}
		})
	}
	for _, query := range []string{"", "SELECT 'SECRET'", "SELECT FROM"} {
		meta := InspectDialect(MustNamer("storage.test"), "Clean", query, nil, DialectSQLite)
		if meta.Diagnostics == nil || len(meta.Diagnostics) != 0 {
			t.Fatal(meta.Diagnostics)
		}
		found := false
		for _, attr := range meta.Attributes("sqlite", "") {
			if attr.Key == QueryDiagnosticsKey {
				found = true
				if attr.Value.Type() != attribute.STRINGSLICE || len(attr.Value.AsStringSlice()) != 0 {
					t.Fatal(attr)
				}
			}
		}
		if !found {
			t.Fatal("missing explicit empty diagnostics array")
		}
	}
}

func TestDatabaseSQLMalformedQueryDiagnostics(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	config := SpanConfig{Tracer: provider.Tracer("diagnostics"), Namer: MustNamer("storage.sqlite"), Dialect: DialectSQLite, System: "sqlite"}
	if _, err := ExecContext(t.Context(), db, config, "SELECT 'SECRET"); err == nil {
		t.Fatal("bad SQLite query succeeded")
	}
	if _, err := ExecContext(t.Context(), db, config, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatal(len(spans))
	}
	for i, span := range spans {
		found := false
		for _, attr := range span.Attributes() {
			if attr.Key == QueryTextKey && i == 0 {
				t.Fatal("unsafe query text was recorded")
			}
			if strings.Contains(attr.Value.Emit(), "SECRET") {
				t.Fatal("query secret leaked")
			}
			if attr.Key == QueryDiagnosticsKey {
				found = true
				got := attr.Value.AsStringSlice()
				if (i == 0 && len(got) != 1) || (i == 1 && len(got) != 0) {
					t.Fatal(got)
				}
			}
		}
		if !found {
			t.Fatal("missing diagnostics on real database span")
		}
	}
}
