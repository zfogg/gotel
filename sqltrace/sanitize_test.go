package sqltrace

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeAdversarialCorpus(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		query   string
		want    string
	}{
		{"postgres escape string", DialectPostgreSQL, `SELECT E'a\'secret\'b', $1`, `SELECT ?, ?`},
		{"postgres ordinary backslash", DialectPostgreSQL, `SELECT 'a\'secret\'b'`, `SELECT ?`},
		{"postgres unicode string", DialectPostgreSQL, `SELECT U&'d\0061ta'`, `SELECT ?`},
		{"postgres dollar quote", DialectPostgreSQL, `SELECT $private$top secret$private$`, `SELECT ?`},
		{"postgres bit and hex", DialectPostgreSQL, `SELECT B'1010', X'deadbeef'`, `SELECT ?, ?`},
		{"nested comments", DialectPostgreSQL, `SELECT /* outer /* inner secret */ hidden */ 7`, `SELECT ?`},
		{"mysql escapes", DialectMySQL, `SELECT 'a\'secret', "other\"secret", _utf8mb4'private' # hidden
FROM t`, `SELECT ?, ?, ? FROM t`},
		{"numeric forms", DialectSQLite, `SELECT 0xCAFE, 0b101, .5, 1_000.25e-2`, `SELECT ?, ?, ?, ?`},
		{"keywords and interval", DialectDuckDB, `SELECT TRUE, false, NULL, INTERVAL '5 days'`, `SELECT ?, ?, ?, INTERVAL ?`},
		{"placeholder forms", DialectGeneric, `SELECT $1, :name, @other, %s, %(value)s, ?12`, `SELECT ?, ?, ?, ?, ?, ?`},
		{"multiple statements", DialectPostgreSQL, `SET application_name = 'secret'; SELECT 42`, `SET application_name = ?; SELECT ?`},
		{"collapse in list", DialectPostgreSQL, `SELECT * FROM t WHERE id IN ($1, $2, $3)`, `SELECT * FROM t WHERE id IN (?)`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := SanitizeDialect(test.dialect, test.query)
			if !ok {
				t.Fatal("safe SQL rejected")
			}
			if got.Text != test.want {
				t.Fatalf("text = %q, want %q", got.Text, test.want)
			}
		})
	}
}

func TestSanitizeFailsClosed(t *testing.T) {
	for _, query := range []string{
		`SELECT 'secret`,
		`SELECT E'secret\`,
		`SELECT $$secret`,
		`SELECT $tag$secret$other$`,
		`SELECT /* secret`,
		`SELECT "secret`,
		"SELECT [secret",
	} {
		if got, ok := SanitizeDialect(DialectPostgreSQL, query); ok || got.Text != "" {
			t.Fatalf("accepted malformed SQL %q as %q", query, got.Text)
		}
	}
}

func TestSanitizeCapsUTF8WithoutSplittingRune(t *testing.T) {
	got, ok := SanitizeWithOptions("SELECT résumé FROM accounts", SanitizeOptions{
		Dialect:           DialectPostgreSQL,
		MaxQueryTextBytes: 10,
	})
	if !ok || !got.Truncated {
		t.Fatalf("result = %#v, ok = %v", got, ok)
	}
	if len(got.Text) > 10 || !utf8.ValidString(got.Text) {
		t.Fatalf("invalid bounded UTF-8 %q (%d bytes)", got.Text, len(got.Text))
	}
}

func FuzzSanitizeDoesNotRetainQuotedPayload(f *testing.F) {
	for _, seed := range []string{"secret", `a\'b`, "résumé", "line\nfeed", "/* comment */", "$tag$"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, payload string) {
		if payload == "" || strings.ContainsAny(payload, "'\\") || !utf8.ValidString(payload) {
			t.Skip()
		}
		for _, dialect := range []Dialect{DialectGeneric, DialectPostgreSQL, DialectDuckDB, DialectSQLite, DialectMySQL} {
			query := "SELECT '" + payload + "'"
			got, ok := SanitizeDialect(dialect, query)
			if !ok {
				t.Fatalf("%s rejected generated quoted literal %q", dialect, query)
			}
			if got.Text != "SELECT ?" {
				t.Fatalf("%s sanitizer retained structure from payload %q in %q", dialect, payload, got.Text)
			}
		}
	})
}

func TestSanitizeCanOmitQueryText(t *testing.T) {
	got, ok := SanitizeWithOptions("SELECT 'secret'", SanitizeOptions{Dialect: DialectPostgreSQL, OmitQueryText: true})
	if !ok || got.Text != "" || got.Truncated {
		t.Fatalf("result = %#v, ok = %v", got, ok)
	}
}

func BenchmarkSanitizeDialects(b *testing.B) {
	statement := "SELECT id, name FROM widgets WHERE tenant_id = $1 AND status IN ('open', 'closed') ORDER BY created_at DESC LIMIT 25"
	for _, dialect := range []Dialect{DialectPostgreSQL, DialectDuckDB, DialectSQLite, DialectMySQL} {
		b.Run(string(dialect), func(b *testing.B) {
			for b.Loop() {
				_, _ = SanitizeDialect(dialect, statement)
			}
		})
	}
}
