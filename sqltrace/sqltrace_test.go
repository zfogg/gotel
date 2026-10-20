package sqltrace

import (
	"context"
	"strings"
	"testing"
)

func TestInspectSQLCQuery(t *testing.T) {
	meta := Inspect(Namer{Prefix: "stonkpile.storage"}, "", "-- name: GetBars :many\nSELECT * FROM bars WHERE symbol = $1 AND n > 42", []any{"secret"})
	if meta.Name != "stonkpile.storage.GetBars" {
		t.Fatalf("name = %q", meta.Name)
	}
	if meta.SanitizedSQL != "SELECT * FROM bars WHERE symbol = ? AND n > ?" {
		t.Fatalf("sanitized SQL = %q", meta.SanitizedSQL)
	}
	if meta.ArgumentCount != 1 {
		t.Fatalf("argument count = %d", meta.ArgumentCount)
	}
}

func TestSanitizeDialects(t *testing.T) {
	input := "/* token */ SELECT 'secret', E'private', $$hidden$$, 42, $1, ? FROM `items` WHERE id = 7 -- no"
	got, ok := Sanitize(input)
	if !ok {
		t.Fatal("valid SQL rejected")
	}
	if want := "SELECT ?, ?, ?, ?, ?, ? FROM `items` WHERE id = ?"; got != want {
		t.Fatalf("Sanitize = %q, want %q", got, want)
	}
	for _, secret := range []string{"token", "secret", "private", "hidden", "42"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized query leaked %q: %q", secret, got)
		}
	}
}

func TestNamerContextAndSuppression(t *testing.T) {
	n := MustNamer("spaniel.storage")
	ctx := n.WithName(context.Background(), "DashboardLatency")
	if got := NameFromContext(ctx); got != "spaniel.storage.DashboardLatency" {
		t.Fatalf("name = %q", got)
	}
	if !TracingDisabled(WithoutTracing(ctx)) {
		t.Fatal("tracing not disabled")
	}
}

func TestNamerConstructionAndQualifiedParsing(t *testing.T) {
	n, err := NewNamer("example.storage")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"FindWidget", "example.storage.FindWidget"} {
		if got, err := n.Parse(input); err != nil || got != "example.storage.FindWidget" {
			t.Fatalf("Parse(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := NewNamer("bad-prefix.storage"); err == nil {
		t.Fatal("invalid prefix accepted")
	}
}

func TestInspectDialectReportsTruncationAndBindCount(t *testing.T) {
	meta := InspectWithOptions(MustNamer("example.storage"), "Find", "SELECT résumé FROM accounts WHERE id = $1", []any{1}, SanitizeOptions{Dialect: DialectPostgreSQL, MaxQueryTextBytes: 10})
	if !meta.QueryTextTruncated {
		t.Fatal("truncation not reported")
	}
	attrs := meta.Attributes("postgresql", "example")
	got := map[string]int64{}
	for _, attr := range attrs {
		if attr.Key == BindCountKey {
			got[string(attr.Key)] = attr.Value.AsInt64()
		}
	}
	if got[string(BindCountKey)] != 1 {
		t.Fatalf("bind count attrs = %#v", got)
	}
}

func TestInspectPreservesQualifiedExplicitName(t *testing.T) {
	namer := MustNamer("example.storage")
	meta := InspectDialect(namer, "example.storage.FindWidget", "SELECT $1", []any{1}, DialectPostgreSQL)
	if meta.Name != "example.storage.FindWidget" {
		t.Fatalf("name = %q", meta.Name)
	}
}

func TestInspectOmitsQueryTextButKeepsMetadata(t *testing.T) {
	meta := InspectWithOptions(MustNamer("example.storage"), "FindWidget", "SELECT * FROM widgets WHERE token = $1", []any{"secret"}, SanitizeOptions{Dialect: DialectPostgreSQL, OmitQueryText: true})
	if meta.Name != "example.storage.FindWidget" || meta.Operation != "SELECT" || meta.ArgumentCount != 1 || meta.SanitizedSQL != "" {
		t.Fatalf("metadata = %#v", meta)
	}
	for _, attr := range meta.Attributes("postgresql", "example") {
		if attr.Key == QueryTextKey {
			t.Fatalf("query text attribute emitted: %#v", attr)
		}
	}
}

func TestInspectionCacheIsBounded(t *testing.T) {
	cache := newInspectionCache(2)
	for _, statement := range []string{"SELECT 1", "SELECT 2", "SELECT 3"} {
		key := inspectionCacheKey{statement: statement}
		cache.put(key, inspectionCacheValue{operation: "SELECT"})
	}
	if cache.order.Len() != 2 || len(cache.entries) != 2 {
		t.Fatalf("cache sizes = %d, %d", cache.order.Len(), len(cache.entries))
	}
	if _, ok := cache.get(inspectionCacheKey{statement: "SELECT 1"}); ok {
		t.Fatal("least recently used entry was not evicted")
	}
}

func BenchmarkInspectCached(b *testing.B) {
	namer := MustNamer("example.storage")
	statement := "-- name: FindWidgets :many\nSELECT * FROM widgets WHERE tenant_id = $1 AND status = $2"
	for b.Loop() {
		_ = InspectDialect(namer, "", statement, []any{1, "active"}, DialectPostgreSQL)
	}
}

func TestSanitizeRejectsUnterminatedContent(t *testing.T) {
	for _, query := range []string{"SELECT 'secret", "SELECT $$secret", "SELECT 1 /* secret"} {
		if _, ok := Sanitize(query); ok {
			t.Fatalf("accepted %q", query)
		}
	}
}

func TestDuckDBSummary(t *testing.T) {
	got := Summary(DialectDuckDB, "SELECT id FROM spans WHERE trace_id = 'secret' ORDER BY start_ns DESC")
	if want := "SELECT … FROM spans WHERE … ORDER BY …"; got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}

func TestCallerNamerFindsOwningFunctionThroughClosure(t *testing.T) {
	caller := CallerNamer("github.com/zfogg/gotel/sqltrace.")
	invoke := func() string { return caller() }
	if got := invoke(); got != "TestCallerNamerFindsOwningFunctionThroughClosure" {
		t.Fatalf("caller name = %q", got)
	}
}

func TestCallerNamerSkipsWrappers(t *testing.T) {
	caller := NewCallerNamer(CallerConfig{
		MatchPrefix: "github.com/zfogg/gotel/sqltrace.",
		Skip:        []string{"invokeCallerWrapper"},
		Fallback:    "Unknown",
	})
	if got := invokeCallerWrapper(caller); got != "TestCallerNamerSkipsWrappers" {
		t.Fatalf("caller name = %q", got)
	}
}

func invokeCallerWrapper(caller func() string) string { return caller() }
