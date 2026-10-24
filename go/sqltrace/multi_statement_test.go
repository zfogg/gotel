package sqltrace

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMultiStatementInspection(t *testing.T) {
	cases := []struct {
		name, query, text, summary, operation string
		count                                 int
	}{
		{"single", "SELECT 1", "SELECT ?", "SELECT …", "SELECT", 1},
		{"set then select", "SET application_name = 'secret'; SELECT 42", "SET application_name = ?; SELECT ?", "SET; SELECT …", "", 2},
		{"three statements", "SELECT 1; UPDATE t SET x='SECRET'; DELETE FROM t WHERE id=2",
			"SELECT ?; UPDATE t SET x=?; DELETE FROM t WHERE id=?", "SELECT …; UPDATE t SET …; DELETE FROM t WHERE …", "", 3},
		{"quoted semicolon", "SELECT ';' AS semi; SELECT 2", "SELECT ? AS semi; SELECT ?", "SELECT …; SELECT …", "SELECT", 2},
		{"empty statements", "SELECT 1;; ; SELECT 2;", "SELECT ?;; ; SELECT ?;", "SELECT …; SELECT …", "SELECT", 2},
		{"nested block comments", "SELECT 1;/* outer ; /* inner ; */ hidden */SELECT 2",
			"SELECT ?; SELECT ?", "SELECT …; SELECT …", "SELECT", 2},
		{"line comment", "SELECT 1; -- hidden ;\nSELECT 2", "SELECT ?; SELECT ?", "SELECT …; SELECT …", "SELECT", 2},
		{"dollar quote", "SELECT $$a;b$$; SELECT 2", "SELECT ?; SELECT ?", "SELECT …; SELECT …", "SELECT", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := InspectDialect(MustNamer("app.storage"), "", tc.query, nil, DialectPostgreSQL)
			if meta.SanitizedSQL != tc.text || meta.Summary != tc.summary || meta.StatementCount != tc.count || meta.Operation != tc.operation {
				t.Fatalf("metadata = %+v; want text=%q summary=%q operation=%q count=%d", meta, tc.text, tc.summary, tc.operation, tc.count)
			}
			found := false
			for _, attr := range meta.Attributes("postgresql", "app") {
				if attr.Key == OperationKey && tc.operation == "" {
					t.Fatal("mixed operations emitted db.operation.name")
				}
				if attr.Key == StatementCountKey {
					found = attr.Value.AsInt64() == int64(tc.count)
				}
			}
			if !found {
				t.Fatal("statement count attribute missing")
			}
		})
	}
}

func TestMultiStatementMalformedLaterStatementFailsClosed(t *testing.T) {
	for _, query := range []string{"SELECT 1; SELECT 'SECRET", "SELECT 1; /* SECRET", "SELECT 1; SELECT $$SECRET"} {
		meta := InspectDialect(MustNamer("app.storage"), "", query, nil, DialectPostgreSQL)
		if meta.SanitizedSQL != "" || meta.StatementCount != 0 {
			t.Fatalf("malformed query leaked metadata: %+v", meta)
		}
	}
}

func TestQueryTextDefault32KiBAndUncappedOption(t *testing.T) {
	query := "SELECT " + strings.Repeat("a", 33*1024) + " FROM t WHERE token='SECRET'"
	bounded, ok := SanitizeDialect(DialectPostgreSQL, query)
	if !ok || !bounded.Truncated || len(bounded.Text) != 32*1024 || !utf8.ValidString(bounded.Text) {
		t.Fatalf("bounded result: length=%d truncated=%v ok=%v", len(bounded.Text), bounded.Truncated, ok)
	}
	uncapped, ok := SanitizeWithOptions(query, SanitizeOptions{Dialect: DialectPostgreSQL, MaxQueryTextBytes: -1})
	if !ok || uncapped.Truncated || !strings.HasSuffix(uncapped.Text, "FROM t WHERE token=?") || strings.Contains(uncapped.Text, "SECRET") {
		t.Fatalf("uncapped result: length=%d truncated=%v ok=%v", len(uncapped.Text), uncapped.Truncated, ok)
	}
	meta := InspectDialect(MustNamer("app.storage"), "", query, nil, DialectPostgreSQL)
	if !meta.QueryTextTruncated || len(meta.SanitizedSQL) != 32*1024 || meta.StatementCount != 1 {
		t.Fatalf("metadata: length=%d truncated=%v count=%d", len(meta.SanitizedSQL), meta.QueryTextTruncated, meta.StatementCount)
	}
}
