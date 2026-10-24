package sqltrace

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode"
)

func TestClickHouseSharedCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/shared/sql_clickhouse.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string   `json:"name"`
		Query     string   `json:"query"`
		Safe      bool     `json:"safe"`
		Operation string   `json:"operation"`
		Contains  []string `json:"contains"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 70 {
		t.Fatal("ClickHouse corpus unexpectedly small")
	}
	compact := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			safe, ok := SanitizeDialect(DialectClickHouse, tc.Query)
			formatted, err := FormatSQL(DialectClickHouse, tc.Query)
			if !tc.Safe {
				if ok || safe.Text != "" || err == nil || formatted != tc.Query {
					t.Fatalf("failed open: %+v, %v", safe, err)
				}
				return
			}
			summary := Summary(DialectClickHouse, tc.Query)
			if !ok || strings.Contains(safe.Text+summary, "SECRET") {
				t.Fatalf("unsafe metadata: %+v %q", safe, summary)
			}
			if operation := statementOperation(DialectClickHouse, tc.Query); operation != tc.Operation {
				t.Fatalf("operation %q, want %q", operation, tc.Operation)
			}
			for _, fragment := range tc.Contains {
				if !strings.Contains(summary, fragment) {
					t.Fatalf("summary %q lacks %q", summary, fragment)
				}
			}
			if err != nil || compact(formatted) != compact(tc.Query) {
				t.Fatalf("format lost content: %q, %v", formatted, err)
			}
			again, err := FormatSQL(DialectClickHouse, formatted)
			if err != nil || again != formatted {
				t.Fatalf("unstable format: %q, %v", again, err)
			}
			after, ok := SanitizeDialect(DialectClickHouse, formatted)
			if !ok || compact(after.Text) != compact(safe.Text) || compact(Summary(DialectClickHouse, formatted)) != compact(summary) {
				t.Fatal("format changed metadata")
			}
			for _, limit := range []int{1, 7, 32} {
				options := DefaultSanitizeOptions(DialectClickHouse)
				options.MaxQueryTextBytes = limit
				bounded, ok := SanitizeWithOptions(tc.Query, options)
				if !ok || len(bounded.Text) > limit || strings.Contains(bounded.Text, "SECRET") {
					t.Fatalf("unsafe bounded text: %+v", bounded)
				}
			}
		})
	}
}

func TestClickHouseLexicalPrivacyAndFormatting(t *testing.T) {
	queries := []string{
		"SELECT 1 // SECRET_VALUE\nFROM t",
		"SELECT 1+// SECRET_VALUE\rFROM hidden\n2 FROM t",
		"SELECT $1tag$SECRET_VALUE$1tag$",
		"SELECT $123$SECRET_VALUE$123$",
		"SELECT ‘SECRET_VALUE’",
		"SELECT‘SECRET_VALUE’",
		"SELECT ‘SECRET_VALUE /* ; ’ FROM t",
		"SELECT 0x1.fp+3, 0xF_F.8p-2",
	}
	for _, query := range queries {
		safe, ok := SanitizeDialect(DialectClickHouse, query)
		if !ok || strings.Contains(safe.Text, "SECRET_VALUE") || strings.Contains(Summary(DialectClickHouse, query), "SECRET_VALUE") {
			t.Fatalf("unsafe ClickHouse metadata: %q", query)
		}
		formatted, err := FormatSQL(DialectClickHouse, query)
		if err != nil {
			t.Fatalf("format %q: %v", query, err)
		}
		if strings.Contains(query, "SECRET_VALUE") && !strings.Contains(formatted, "SECRET_VALUE") {
			t.Fatalf("formatter altered lexical content: %q", formatted)
		}
		again, err := FormatSQL(DialectClickHouse, formatted)
		if err != nil || again != formatted || Summary(DialectClickHouse, query) != Summary(DialectClickHouse, formatted) {
			t.Fatalf("unstable ClickHouse formatting: %q", formatted)
		}
	}
	safe, ok := SanitizeDialect(DialectClickHouse, "SELECT 0x1.fp+3")
	if !ok || safe.Text != "SELECT ?" {
		t.Fatalf("hex float not completely sanitized: %q", safe.Text)
	}
}
