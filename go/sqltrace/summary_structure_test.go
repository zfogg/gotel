package sqltrace

import (
	sequel "github.com/zfogg/sequel/go"
	"strings"
	"testing"
)

// TestSummaryRowBounds checks scope, static counts and noninterpolation independently.
func TestSummaryRowBounds(t *testing.T) {
	cases := []struct{ sql, want string }{
		{"INSERT INTO a SELECT unnest($1) ON CONFLICT(id) DO UPDATE SET id=excluded.id", "INSERT INTO a SELECT unnest(…) ON CONFLICT DO UPDATE SET …"},
		{"INSERT INTO a SELECT id FROM b WHERE b.active=true ON CONFLICT(id) DO NOTHING RETURNING id", "INSERT INTO a SELECT … FROM b WHERE … ON CONFLICT DO NOTHING RETURNING …"},
		{"SELECT * FROM a LIMIT 20 OFFSET 0", "SELECT … FROM a LIMIT 20 OFFSET 0"},
		{"SELECT * FROM a LIMIT $1 OFFSET $2", "SELECT … FROM a LIMIT ? OFFSET ?"},
		{"SELECT * FROM a LIMIT sqlc.arg(page_size)", "SELECT … FROM a LIMIT ?"},
		{"SELECT * FROM a LIMIT (SELECT cap FROM settings LIMIT 1)", "SELECT … FROM a LIMIT ? (SELECT … FROM settings LIMIT 1)"},
		{"SELECT * FROM a LIMIT 10 + 777", "SELECT … FROM a LIMIT ?"},
		{"SELECT * FROM a LIMIT ALL", "SELECT … FROM a LIMIT ALL"},
		{"SELECT * FROM a LIMIT 5, ?", "SELECT … FROM a LIMIT 5, ?"},
		{"SELECT * FROM a OFFSET 2 ROWS FETCH NEXT $1 ROWS ONLY", "SELECT … FROM a OFFSET 2 FETCH NEXT ? ROWS ONLY"},
		{"SELECT * FROM a FETCH FIRST 3 ROWS WITH TIES", "SELECT … FROM a FETCH FIRST 3 ROWS WITH TIES"},
		{"SELECT * FROM a FETCH FIRST ROW ONLY", "SELECT … FROM a FETCH FIRST 1 ROWS ONLY"},
		{"UPDATE a SET token='secret' WHERE id=777 ORDER BY id LIMIT ?", "UPDATE a SET … WHERE … ORDER BY … LIMIT ?"},
		{"DELETE FROM a WHERE id=777 ORDER BY id LIMIT 5", "DELETE FROM a WHERE … ORDER BY … LIMIT 5"},
		{"(SELECT * FROM a LIMIT 1) UNION ALL (SELECT * FROM b LIMIT $1) ORDER BY id LIMIT 10", "(SELECT … FROM a LIMIT 1) UNION ALL (SELECT … FROM b LIMIT ?) ORDER BY … LIMIT 10"},
		{"SELECT * FROM a UNION SELECT * FROM b INTERSECT ALL SELECT * FROM c EXCEPT SELECT * FROM d LIMIT ?", "SELECT … FROM a UNION SELECT … FROM b INTERSECT ALL SELECT … FROM c EXCEPT SELECT … FROM d LIMIT ?"},
	}
	for _, tc := range cases {
		t.Run(tc.sql, func(t *testing.T) {
			if got := sequel.Summary(DialectPostgreSQL, tc.sql); got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
	meta := Inspect(MustNamer("test.storage"), "Find", "SELECT 777 FROM a WHERE token='secret' LIMIT $1", []any{999})
	if meta.Summary != "SELECT … FROM a WHERE … LIMIT ?" || strings.Contains(meta.Summary, "999") || strings.Contains(meta.Summary, "777") || strings.Contains(meta.Summary, "secret") {
		t.Fatalf("unsafe summary: %+v", meta)
	}
	// Numeric preservation is private to structural bounds, never query text.
	safe, ok := sequel.SanitizeWithOptions("SELECT 777 FROM a LIMIT 20", sequel.DefaultSanitizeOptions(DialectPostgreSQL))
	if !ok || safe.Text != "SELECT ? FROM a LIMIT ?" {
		t.Fatalf("query text privacy changed: %+v", safe)
	}
}

// TestSummaryJoinKinds checks every supported join spelling without deduplication.
func TestJoinSummaryMetadata(t *testing.T) {
	query := `SELECT * FROM accounts a JOIN orders o ON o.account_id=a.id AND o.token=$1 AND o.cost>123 LIMIT $2`
	want := "SELECT … FROM accounts AS a JOIN orders AS o ON o.account_id=a.id AND o.token=? AND o.cost>? LIMIT ?"
	for _, omit := range []bool{false, true} {
		options := sequel.DefaultSanitizeOptions(DialectPostgreSQL)
		options.OmitQueryText = omit
		options.MaxQueryTextBytes = 12
		meta := InspectWithOptions(MustNamer("ratio.storage"), "ListAccountOrders", query, []any{"secret", 10}, options)
		if meta.Name != "ratio.storage.ListAccountOrders" || meta.Summary != want || meta.ArgumentCount != 2 {
			t.Fatalf("unexpected metadata: %+v", meta)
		}
		found := false
		for _, attr := range meta.Attributes("postgresql", "ratio") {
			if attr.Key == QuerySummaryKey {
				found = attr.Value.AsString() == want
			}
		}
		if !found {
			t.Fatal("missing summary attribute")
		}
	}
}
