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
	n := Namer{Prefix: "spaniel.storage"}
	ctx := n.WithName(context.Background(), "DashboardLatency")
	if got := NameFromContext(ctx); got != "spaniel.storage.DashboardLatency" {
		t.Fatalf("name = %q", got)
	}
	if !TracingDisabled(WithoutTracing(ctx)) {
		t.Fatal("tracing not disabled")
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
	if want := "SELECT id FROM spans WHERE trace_id = ? ORDER BY start_ns DESC · 1 args"; got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}
