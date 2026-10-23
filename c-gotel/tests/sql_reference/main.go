// Regenerate from the Go implementation: go run ./c-gotel/tests/sql_reference
package main

import (
	"encoding/json"
	"github.com/zfogg/gotel/sqltrace"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
)

type example struct {
	Query     string `json:"query"`
	Dialect   int    `json:"dialect"`
	Safe      bool   `json:"safe"`
	Text      string `json:"text"`
	Summary   string `json:"summary"`
	Operation string `json:"operation"`
}

func main() {
	if len(os.Args) == 3 {
		b, err := os.ReadFile(os.Args[1])
		if err != nil {
			panic(err)
		}
		var examples []example
		if err = json.Unmarshal(b, &examples); err != nil {
			panic(err)
		}
		dialects := []sqltrace.Dialect{sqltrace.DialectGeneric, sqltrace.DialectSQLite, sqltrace.DialectPostgreSQL, sqltrace.DialectDuckDB, sqltrace.DialectMySQL}
		for i := range examples {
			e := &examples[i]
			d := dialects[e.Dialect]
			safe, ok := sqltrace.SanitizeDialect(d, e.Query)
			m := sqltrace.InspectDialect(sqltrace.MustNamer("test"), "", e.Query, nil, d)
			e.Safe = ok
			e.Text = safe.Text
			e.Summary = m.Summary
			e.Operation = m.Operation
		}
		b, err = json.MarshalIndent(examples, "", "  ")
		if err != nil {
			panic(err)
		}
		if err = os.WriteFile(os.Args[2], append(b, '\n'), 0644); err != nil {
			panic(err)
		}
		return
	}
	seen := map[string]bool{}
	for _, query := range []string{
		"CREATE TABLE accounts (id INTEGER PRIMARY KEY, token TEXT DEFAULT 'secret')",
		"CREATE INDEX account_id ON accounts(id)",
		"ALTER TABLE accounts ADD COLUMN secret TEXT",
		"DROP TABLE accounts", "REINDEX accounts", "PRAGMA journal_mode=WAL",
		"VACUUM accounts", "ANALYZE accounts", "TRUNCATE TABLE accounts",
		"COPY accounts FROM '/private/file.csv'", "BEGIN TRANSACTION", "COMMIT", "ROLLBACK",
	} {
		seen[query] = true
	}
	for _, file := range []string{"sqltrace/sanitize_test.go", "sqltrace/summary_test.go", "sqltrace/summary_structure_test.go", "sqltrace/sqltrace_test.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			panic(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			q, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			u := strings.ToUpper(strings.TrimSpace(q))
			if strings.Contains(q, "…") {
				return true
			}
			for _, op := range []string{"SELECT ", "WITH ", "INSERT ", "UPDATE ", "DELETE ", "MERGE ", "EXPLAIN ", "SET ", "PRAGMA ", "CREATE ", "(SELECT ", "-- NAME:"} {
				if strings.HasPrefix(u, op) {
					seen[q] = true
					break
				}
			}
			return true
		})
	}
	var queries []string
	for q := range seen {
		queries = append(queries, q)
	}
	sort.Strings(queries)
	dialects := []sqltrace.Dialect{sqltrace.DialectGeneric, sqltrace.DialectSQLite, sqltrace.DialectPostgreSQL, sqltrace.DialectDuckDB, sqltrace.DialectMySQL}
	var out []example
	for _, q := range queries {
		for i, d := range dialects {
			safe, ok := sqltrace.SanitizeDialect(d, q)
			m := sqltrace.InspectDialect(sqltrace.MustNamer("test"), "", q, nil, d)
			out = append(out, example{q, i, ok, safe.Text, m.Summary, m.Operation})
		}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile("c-gotel/tests/sql_reference.json", append(b, '\n'), 0644); err != nil {
		panic(err)
	}
}
