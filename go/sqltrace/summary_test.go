package sqltrace

import (
	sequel "github.com/zfogg/sequel/go"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSummaryPoliciesAndBounds(t *testing.T) {
	if meta := Inspect(MustNamer("example.storage"), "", "", nil); meta.Name != "example.storage.Unknown" || meta.Operation != "" {
		t.Fatalf("empty statement lost Unknown fallback: %+v", meta)
	}
	query := "-- name: FindBars :many\nWITH recent AS (SELECT * FROM bars WHERE ts>$1) SELECT * FROM recent"
	options := sequel.DefaultSanitizeOptions(DialectPostgreSQL)
	options.OmitQueryText = true
	meta := InspectWithOptions(MustNamer("example.storage"), "", query, []any{"secret"}, options)
	if meta.Summary != "WITH recent AS (SELECT … FROM bars WHERE …) SELECT … FROM recent" || meta.Operation != "SELECT" || meta.SanitizedSQL != "" || meta.Name != "example.storage.FindBars" {
		t.Fatalf("%+v", meta)
	}
	options.OmitQueryText = false
	options.MaxQueryTextBytes = 5
	if m := InspectWithOptions(MustNamer("example.storage"), "", query, nil, options); m.Summary != meta.Summary {
		t.Fatalf("summary depends on query text truncation: %+v", m)
	}
	summary := sequel.Summary(DialectPostgreSQL, "SELECT * FROM "+strings.Repeat("表", 12000))
	if len(summary) > DefaultMaxSummaryBytes || !utf8.ValidString(summary) || !strings.HasSuffix(summary, "…") {
		t.Fatalf("unbounded/invalid summary: %q", summary)
	}
	if got := sequel.Summary(DialectGeneric, "SELECT * FROM a,b,c,d,e,f,g"); got != "SELECT … FROM a, b, c, d, e, f, g" {
		t.Fatal(got)
	}
}

// TestStonkpileQuerySummaries exercises the complete checked-in query corpus,
// including sqlc source syntax. The snapshot deliberately includes every name:
// additions and summary regressions require an explicit review.
