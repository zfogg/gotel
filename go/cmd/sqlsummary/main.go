// Command sqlsummary shows gotel summaries of actual generated sqlc statements.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/zfogg/gotel/go/sqltrace"
)

// query records one generated SQL constant and its source location.
type query struct {
	Name    string `json:"name"`
	Bytes   int    `json:"sql_bytes"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Summary string `json:"summary"`
}

// main loads sqlc output without importing, compiling or executing the app.
func main() {
	dir := flag.String("dir", ".", "directory containing generated *.sql.go files")
	largest := flag.Int("largest", 10, "number of largest statements to show (0 = all)")
	dialect := flag.String("dialect", "postgresql", "SQL dialect")
	output := flag.String("out", "", "write report to this file instead of stdout")
	asJSON := flag.Bool("json", false, "emit JSON instead of a text report")
	flag.Parse()
	if err := run(*dir, *largest, sqltrace.Dialect(*dialect), *output, *asJSON); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run emits deterministic summaries ranked by SQL byte size, then query name.
func run(dir string, largest int, dialect sqltrace.Dialect, output string, asJSON bool) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql.go"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no generated sqlc files in %s", dir)
	}
	var queries []query
	for _, path := range files {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			group, ok := decl.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, spec := range group.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, expr := range value.Values {
					literal, ok := expr.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					sql, err := strconv.Unquote(literal.Value)
					if err != nil {
						return err
					}
					name := sqltrace.SQLCName(sql)
					if name == "" {
						continue
					}
					queries = append(queries, query{Name: name, Bytes: len(sql), File: filepath.Base(path), Line: set.Position(literal.Pos()).Line, Summary: sqltrace.Summary(dialect, sql)})
				}
			}
		}
	}
	sort.Slice(queries, func(i, j int) bool {
		if queries[i].Bytes == queries[j].Bytes {
			return queries[i].Name < queries[j].Name
		}
		return queries[i].Bytes > queries[j].Bytes
	})
	if largest > 0 && len(queries) > largest {
		queries = queries[:largest]
	}
	var writer io.Writer = os.Stdout
	if output != "" {
		file, err := os.Create(output)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = file
	}
	if asJSON {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(queries)
	}
	for i, q := range queries {
		if _, err := fmt.Fprintf(writer, "%d. %s — %d SQL bytes (%s:%d)\n%s\n\n", i+1, q.Name, q.Bytes, q.File, q.Line, q.Summary); err != nil {
			return err
		}
	}
	return nil
}
