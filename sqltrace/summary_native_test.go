package sqltrace

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNativeSummaryEdgeCorpus(t *testing.T) {
	data, err := os.ReadFile("../c-gotel/tests/sql_edge_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string   `json:"name"`
		Dialect   Dialect  `json:"dialect"`
		Query     string   `json:"query"`
		Operation string   `json:"operation"`
		Contains  []string `json:"contains"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 40 {
		t.Fatalf("edge corpus unexpectedly small: %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			summary := Summary(tc.Dialect, tc.Query)
			if operation := statementOperation(tc.Dialect, tc.Query); operation != tc.Operation {
				t.Errorf("operation = %q, want %q", operation, tc.Operation)
			}
			for _, fragment := range tc.Contains {
				if !strings.Contains(summary, fragment) {
					t.Errorf("summary %q lacks %q", summary, fragment)
				}
			}
			if strings.Contains(summary, "SECRET") || !utf8.ValidString(summary) || len(summary) > DefaultMaxSummaryBytes {
				t.Errorf("unsafe summary %q", summary)
			}
		})
	}
}

func TestNativeSummaryHistoricalParity(t *testing.T) {
	// These fixed snapshots predate the cgo switch and include five dialects
	// plus production queries captured from Spaniel.
	for _, path := range []string{"../c-gotel/tests/sql_reference.json", "../c-gotel/tests/stonkpile_pi5.json"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cases []struct {
			Query     string `json:"query"`
			Dialect   int    `json:"dialect"`
			Summary   string `json:"summary"`
			Operation string `json:"operation"`
		}
		if err := json.Unmarshal(data, &cases); err != nil {
			t.Fatal(err)
		}
		dialects := []Dialect{DialectGeneric, DialectSQLite, DialectPostgreSQL, DialectDuckDB, DialectMySQL}
		for i, tc := range cases {
			if tc.Dialect < 0 || tc.Dialect >= len(dialects) {
				t.Fatalf("%s[%d]: invalid dialect %d", path, i, tc.Dialect)
			}
			d := dialects[tc.Dialect]
			if got := Summary(d, tc.Query); got != tc.Summary {
				t.Errorf("%s[%d]: summary = %q, want %q", path, i, got, tc.Summary)
			}
			if got := statementOperation(d, tc.Query); got != tc.Operation {
				t.Errorf("%s[%d]: operation = %q, want %q", path, i, got, tc.Operation)
			}
		}
	}
}

func TestNativeSummaryLargeStructures(t *testing.T) {
	var ctes strings.Builder
	ctes.WriteString("WITH ")
	for i := range 60 {
		if i > 0 {
			ctes.WriteString(", ")
		}
		ctes.WriteString("c")
		ctes.WriteString(strings.Repeat("x", i+1))
		ctes.WriteString(" AS (SELECT id FROM records WHERE token='SECRET')")
	}
	ctes.WriteString(" SELECT * FROM cxxxxxxxxxx")
	query := ctes.String()
	if len(query) < 4000 {
		t.Fatalf("large fixture too short: %d", len(query))
	}
	for _, dialect := range []Dialect{DialectGeneric, DialectSQLite, DialectPostgreSQL, DialectDuckDB, DialectMySQL} {
		summary := Summary(dialect, query)
		if !strings.HasPrefix(summary, "WITH ") || !strings.Contains(summary, "SELECT … FROM records WHERE …") ||
			strings.Contains(summary, "SECRET") || len(summary) > DefaultMaxSummaryBytes || !utf8.ValidString(summary) {
			t.Errorf("%s: invalid large summary: %q", dialect, summary)
		}
	}
	if got := Summary(DialectPostgreSQL, "SELECT "+strings.Repeat("表", 500000)); got != "SQL" {
		t.Fatalf("over-size query = %q, want SQL", got)
	}
}
