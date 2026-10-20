package sqltrace

import (
	"strings"
	"testing"
)

// TestSummaryRowBounds checks scope, static counts and noninterpolation independently.
func TestSummaryRowBounds(t *testing.T) {
	cases := []struct{ sql, want string }{
		{"INSERT INTO a SELECT unnest($1) ON CONFLICT(id) DO UPDATE SET id=excluded.id", "INSERT INTO a SELECT unnest ON CONFLICT DO UPDATE SET …"},
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
		want := "SELECT … FROM accounts AS a " + join + " (SELECT … FROM accounts WHERE … LIMIT 1) AS b ON a.id=b.id LIMIT ?"
		if got := Summary(DialectDuckDB, sql); got != want {
			t.Errorf("%s: got %q; want %q", join, got, want)
		}
	}
}

// TestSummaryJoinConditions verifies predicates, aliases and nested scopes
// independently of the corpus snapshot, including private literal replacement.
func TestSummaryJoinConditions(t *testing.T) {
	cases := []struct{ sql, want string }{
		{"SELECT * FROM a x LEFT JOIN b y ON x.id = y.a_id AND y.secret = 'sentinel' AND y.n >= 987654 AND y.f < .8765 JOIN c z USING (id, tenant_id) LIMIT 5", "SELECT … FROM a AS x LEFT JOIN b AS y ON x.id = y.a_id AND y.secret = ? AND y.n >= ? AND y.f < ? JOIN c AS z USING (id, tenant_id) LIMIT 5"},
		{"SELECT * FROM a JOIN b ON CASE WHEN b.n > 123 THEN a.id ELSE b.id END = a.id LEFT JOIN c ON c.id IS NOT DISTINCT FROM b.id", "SELECT … FROM a JOIN b ON CASE WHEN b.n > ? THEN a.id ELSE b.id END = a.id LEFT JOIN c ON c.id IS NOT DISTINCT FROM b.id"},
		{"SELECT * FROM a JOIN b ON b.id IN (SELECT id FROM c JOIN d USING(id) LIMIT 1) AND b.ts >= now() - interval 'secret'", "SELECT … FROM a JOIN b ON b.id IN (SELECT … FROM c JOIN d USING (id) LIMIT 1) AND b.ts >= now() - interval ?"},
		{"SELECT * FROM a JOIN b ON coalesce(a.id, 444) = b.id, c", "SELECT … FROM a JOIN b ON coalesce(a.id, ?) = b.id, c"},
		{"SELECT * FROM a AS \"left\" JOIN b AS \"right\" ON \"left\".\"key\"=\"right\".\"key\" AND b.v=1.23e45::numeric", "SELECT … FROM a AS \"left\" JOIN b AS \"right\" ON \"left\".\"key\"=\"right\".\"key\" AND b.v=?::numeric"},
		{"SELECT * FROM a JOIN b ON (b.id=a.id AND (b.x BETWEEN -987 AND +654 OR b.v=$1))", "SELECT … FROM a JOIN b ON (b.id=a.id AND (b.x BETWEEN -? AND +? OR b.v=?))"},
		{"SELECT * FROM a JOIN LATERAL (SELECT * FROM b LIMIT $1) AS recent ON true UNION ALL SELECT * FROM c JOIN d USING(id)", "SELECT … FROM a JOIN LATERAL (SELECT … FROM b LIMIT ?) AS recent ON ? UNION ALL SELECT … FROM c JOIN d USING (id)"},
	}
	for _, dialect := range []Dialect{DialectPostgreSQL, DialectDuckDB, DialectSQLite, DialectGeneric} {
		for _, tc := range cases {
			t.Run(string(dialect)+"/"+tc.sql, func(t *testing.T) {
				if got := Summary(dialect, tc.sql); got != tc.want {
					t.Fatalf("got %q; want %q", got, tc.want)
				}
			})
		}
	}
	query := "SELECT * FROM a JOIN b ON b.secret=$$sentinel$$ AND b.token=E'secret' AND b.number=0xDEAD AND b.flag=true /* hidden */"
	if got := Summary(DialectPostgreSQL, query); got != "SELECT … FROM a JOIN b ON b.secret=? AND b.token=? AND b.number=? AND b.flag=?" {
		t.Fatal(got)
	}
	if got := Summary(DialectMySQL, `SELECT * FROM a JOIN b ON b.secret="sentinel" AND b.token=? AND b.n=987`); got != "SELECT … FROM a JOIN b ON b.secret=? AND b.token=? AND b.n=?" {
		t.Fatal(got)
	}
	for _, dialect := range []Dialect{DialectPostgreSQL, DialectDuckDB} {
		if got := Summary(dialect, `SELECT * FROM unnest($1::text[]) WITH ORDINALITY AS x(id, n) JOIN unnest($2::text[]) WITH ORDINALITY AS y(id, n) USING(n)`); got != "SELECT … FROM unnest(…) WITH ORDINALITY AS x(id, n) JOIN unnest(…) WITH ORDINALITY AS y(id, n) USING (n)" {
			t.Fatalf("%s: %s", dialect, got)
		}
		query := `SELECT * FROM a JOIN b ON b.tags = ARRAY['secret', 'private'] AND b.ids[123] = 456`
		if got := Summary(dialect, query); got != "SELECT … FROM a JOIN b ON b.tags = ARRAY[?, ?] AND b.ids[?] = ?" {
			t.Fatalf("%s: %s", dialect, got)
		}
		query = `INSERT INTO a SELECT unnest(CASE WHEN cardinality($1::text[]) = 0 THEN ARRAY[cardinality($2::text[])] ELSE $1 END) ON CONFLICT(id) DO NOTHING`
		if got := Summary(dialect, query); got != "INSERT INTO a SELECT unnest ON CONFLICT DO NOTHING" {
			t.Fatalf("%s: %s", dialect, got)
		}
	}
}

// TestJoinSummaryMetadata checks exported attributes without changing span identity
// or allowing query-text truncation/omission to erase the structural summary.
func TestJoinSummaryMetadata(t *testing.T) {
	query := `SELECT * FROM accounts a JOIN orders o ON o.account_id=a.id AND o.token=$1 AND o.cost>123 LIMIT $2`
	want := "SELECT … FROM accounts AS a JOIN orders AS o ON o.account_id=a.id AND o.token=? AND o.cost>? LIMIT ?"
	for _, omit := range []bool{false, true} {
		options := DefaultSanitizeOptions(DialectPostgreSQL)
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
