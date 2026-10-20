package sqltrace

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var updateSummaries = flag.Bool("update-summaries", false, "regenerate the reviewed Stonkpile summary snapshot")

func TestSummary(t *testing.T) {
	cases := []struct{ query, want string }{
		{"-- name: FindAccount :one\nSELECT id FROM accounts WHERE token='secret'", "SELECT accounts"},
		{"SELECT a.id FROM accounts a JOIN holdings h ON h.account_id=a.id WHERE EXISTS (SELECT 1 FROM orders o WHERE o.account_id=a.id)", "SELECT accounts, holdings, orders"},
		{"WITH candidates AS MATERIALIZED (SELECT id FROM signals WHERE id>$1), picked AS (SELECT * FROM candidates) DELETE FROM signals USING picked WHERE signals.id=picked.id", "DELETE signals SELECT signals"},
		{"WITH RECURSIVE tree(id) AS (SELECT id FROM nodes UNION ALL SELECT n.id FROM nodes n JOIN tree t ON n.parent=t.id) SELECT * FROM tree", "SELECT nodes"},
		{"WITH x AS (SELECT * FROM public.accounts) SELECT * FROM (WITH x AS (SELECT * FROM holdings) SELECT * FROM x) q JOIN x ON true", "SELECT holdings, public.accounts"},
		{"INSERT INTO results (id) SELECT id FROM signals ON CONFLICT(id) DO UPDATE SET id=excluded.id RETURNING id", "INSERT results SELECT signals"},
		{"INSERT INTO results(id) VALUES($1) ON CONFLICT(id) DO UPDATE SET id=excluded.id WHERE results.id IS DISTINCT FROM excluded.id", "INSERT results"},
		{"WITH gone AS (DELETE FROM events RETURNING *) INSERT INTO archive SELECT * FROM gone", "INSERT archive; DELETE events"},
		{"WITH changed AS (UPDATE accounts SET active=true RETURNING *) SELECT * FROM changed", "SELECT; UPDATE accounts"},
		{"SELECT * FROM accounts UNION SELECT * FROM archived_accounts EXCEPT SELECT * FROM blocked_accounts", "SELECT accounts, archived_accounts, blocked_accounts"},
		{"SELECT row_number() OVER (PARTITION BY symbol ORDER BY ts), sum(price) FILTER (WHERE price>0) FROM bars", "SELECT bars"},
		{"SELECT prepare_signal_grading($1, $2, $3, $4)", "SELECT prepare_signal_grading"},
		{"SELECT public.prepare_signal_grading($1, $2)", "SELECT public.prepare_signal_grading"},
		{"SELECT pg_try_advisory_lock(hashtextextended($1,0))::bool", "SELECT pg_try_advisory_lock"},
		{"SELECT s.id FROM signals s JOIN LATERAL (SELECT * FROM bars WHERE symbol=s.symbol LIMIT 1) b ON true", "SELECT signals, bars"},
		{"UPDATE accounts SET balance=(SELECT sum(delta) FROM ledger) FROM users WHERE users.id=accounts.id", "UPDATE accounts SELECT ledger, users"},
		{"MERGE INTO inventory i USING changes c ON i.id=c.id WHEN MATCHED THEN UPDATE SET quantity=c.quantity", "MERGE inventory SELECT changes"},
		{"EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM bars", "EXPLAIN SELECT bars"},
		{"SELECT * FROM \"Reporting\".\"Order History\" AS \"FROM\" JOIN \"JOIN\" ON true", "SELECT \"Reporting\".\"Order History\", \"JOIN\""},
		{"SELECT * FROM a, b, c WHERE a.id=b.id", "SELECT a, b, c"},
		{"SELECT * FROM read_parquet('secret/path')", "SELECT read_parquet"},
		{"SELECT 'secret FROM passwords' AS value", "SELECT"},
		{"SELECT 'unterminated", "SQL"},
		{"EXPLAIN (", "EXPLAIN"},
		{"COPY \"public\".\"bars\" FROM STDIN", "COPY \"public\".\"bars\""},
	}
	for _, dialect := range []Dialect{DialectPostgreSQL, DialectDuckDB, DialectSQLite, DialectMySQL, DialectGeneric} {
		for _, tc := range cases {
			if dialect == DialectMySQL && strings.Contains(tc.query, "\"") {
				continue
			} // MySQL double quotes are literals.
			t.Run(string(dialect)+"/"+tc.query, func(t *testing.T) {
				if got := Summary(dialect, tc.query); got != tc.want {
					t.Fatalf("got %q; want %q", got, tc.want)
				}
			})
		}
	}
}

func TestSummaryPoliciesAndBounds(t *testing.T) {
	if meta := Inspect(MustNamer("example.storage"), "", "", nil); meta.Name != "example.storage.Unknown" || meta.Operation != "" {
		t.Fatalf("empty statement lost Unknown fallback: %+v", meta)
	}
	query := "-- name: FindBars :many\nWITH recent AS (SELECT * FROM bars WHERE ts>$1) SELECT * FROM recent"
	options := DefaultSanitizeOptions(DialectPostgreSQL)
	options.OmitQueryText = true
	meta := InspectWithOptions(MustNamer("example.storage"), "", query, []any{"secret"}, options)
	if meta.Summary != "SELECT bars" || meta.Operation != "SELECT" || meta.SanitizedSQL != "" || meta.Name != "example.storage.FindBars" {
		t.Fatalf("%+v", meta)
	}
	options.OmitQueryText = false
	options.MaxQueryTextBytes = 5
	if m := InspectWithOptions(MustNamer("example.storage"), "", query, nil, options); m.Summary != meta.Summary {
		t.Fatalf("summary depends on query text truncation: %+v", m)
	}
	summary := Summary(DialectPostgreSQL, "SELECT * FROM "+strings.Repeat("表", 150))
	if len(summary) > 256 || !utf8.ValidString(summary) {
		t.Fatalf("unbounded/invalid summary: %q", summary)
	}
	if got := Summary(DialectGeneric, "SELECT * FROM a,b,c,d,e,f,g"); got != "SELECT a, b, c, d, e, …" {
		t.Fatal(got)
	}
}

// TestStonkpileQuerySummaries exercises the complete checked-in query corpus,
// including sqlc source syntax. The snapshot deliberately includes every name:
// additions and summary regressions require an explicit review.
func TestStonkpileQuerySummaries(t *testing.T) {
	files, err := filepath.Glob("testdata/stonkpile/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("missing SQL corpus", err)
	}
	header := regexp.MustCompile("(?m)^-- name: ([A-Za-z0-9_]+) :[^\r\n]+")
	var output strings.Builder
	count := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		matches := header.FindAllSubmatchIndex(data, -1)
		for i, match := range matches {
			end := len(data)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			query := string(data[match[0]:end])
			name := string(data[match[2]:match[3]])
			summary := Summary(DialectPostgreSQL, query)
			if summary == "SQL" || summary == "SELECT" || strings.HasPrefix(summary, "WITH") {
				t.Errorf("%s: unhelpful summary %q", name, summary)
			}
			output.WriteString(filepath.Base(file) + " / " + name + " => " + summary + "\n")
			count++
		}
	}
	path := "testdata/stonkpile_summaries.txt"
	if *updateSummaries {
		if err := os.WriteFile(path, []byte(output.String()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != output.String() {
		t.Fatal("summary snapshot changed; inspect output before regenerating with -update-summaries")
	}
	t.Logf("covered %d named queries across %d files", count, len(files))
}

func FuzzSummary(f *testing.F) {
	f.Add("WITH t AS (SELECT * FROM bars) SELECT * FROM t")
	f.Add("SELECT $$secret$$")
	f.Fuzz(func(t *testing.T, query string) {
		if len(query) > 65536 {
			t.Skip()
		}
		for _, d := range []Dialect{DialectGeneric, DialectPostgreSQL, DialectDuckDB, DialectSQLite, DialectMySQL} {
			got := Summary(d, query)
			if got == "" || len(got) > 256 || (utf8.ValidString(query) && !utf8.ValidString(got)) {
				t.Fatalf("%s: invalid summary %q", d, got)
			}
		}
	})
}

func BenchmarkSummaryComplexCTE(b *testing.B) {
	query := "WITH candidates AS MATERIALIZED (SELECT id FROM signals WHERE ts>$1), removed AS (DELETE FROM signal_outcomes USING candidates WHERE signal_id=candidates.id RETURNING signal_id) SELECT s.id FROM signals s JOIN candidates c ON c.id=s.id WHERE NOT EXISTS (SELECT 1 FROM tradebot_intents i WHERE i.signal_id=s.id)"
	for b.Loop() {
		_ = Summary(DialectPostgreSQL, query)
	}
}
