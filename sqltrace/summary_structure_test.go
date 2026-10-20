package sqltrace

import (
	"strings"
	"testing"
)

// TestSummaryRowBounds checks scope, static counts and noninterpolation independently.
func TestSummaryRowBounds(t *testing.T) {
	cases := []struct{ sql, want string }{
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
			if got := Summary(DialectPostgreSQL, tc.sql); got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
	meta := Inspect(MustNamer("test.storage"), "Find", "SELECT 777 FROM a WHERE token='secret' LIMIT $1", []any{999})
	if meta.Summary != "SELECT … FROM a WHERE … LIMIT ?" || strings.Contains(meta.Summary, "999") || strings.Contains(meta.Summary, "777") || strings.Contains(meta.Summary, "secret") {
		t.Fatalf("unsafe summary: %+v", meta)
	}
	// Numeric preservation is private to structural bounds, never query text.
	safe, ok := SanitizeWithOptions("SELECT 777 FROM a LIMIT 20", DefaultSanitizeOptions(DialectPostgreSQL))
	if !ok || safe.Text != "SELECT ? FROM a LIMIT ?" {
		t.Fatalf("query text privacy changed: %+v", safe)
	}
}

// TestSummaryJoinKinds checks every supported join spelling without deduplication.
func TestSummaryJoinKinds(t *testing.T) {
	for _, join := range []string{"JOIN", "INNER JOIN", "LEFT JOIN", "LEFT OUTER JOIN", "RIGHT JOIN", "FULL OUTER JOIN", "CROSS JOIN", "NATURAL JOIN", "NATURAL LEFT JOIN", "SEMI JOIN", "ANTI JOIN", "ASOF LEFT JOIN", "POSITIONAL JOIN", "JOIN LATERAL", "LEFT JOIN LATERAL", "CROSS JOIN LATERAL"} {
		sql := "SELECT * FROM accounts a " + join + " (SELECT * FROM accounts WHERE token='secret' LIMIT 1) b ON a.id=b.id LIMIT $1"
		want := "SELECT … FROM accounts " + join + " (SELECT … FROM accounts WHERE … LIMIT 1) LIMIT ?"
		if got := Summary(DialectDuckDB, sql); got != want {
			t.Errorf("%s: got %q; want %q", join, got, want)
		}
	}
}
