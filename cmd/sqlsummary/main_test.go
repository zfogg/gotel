package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zfogg/gotel/sqltrace"
)

// TestRun ranks generated SQL, ignores unrelated constants and never executes it.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	source := "package db\nconst ignored = `not sql`\nconst small = `-- name: Small :one\nSELECT 1`\nconst large = `-- name: Large :many\nSELECT * FROM accounts LEFT JOIN LATERAL (SELECT * FROM events LIMIT 1) e ON true LIMIT $1`\n"
	if err := os.WriteFile(filepath.Join(dir, "queries.sql.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "report.json")
	if err := run(dir, 1, sqltrace.DialectPostgreSQL, output, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var rows []query
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Large" || rows[0].Line != 5 || rows[0].Summary != "SELECT … FROM accounts LEFT JOIN LATERAL (SELECT … FROM events LIMIT 1) LIMIT ?" {
		t.Fatalf("%+v", rows)
	}
	if err := run(filepath.Join(dir, "absent"), 10, sqltrace.DialectPostgreSQL, "", false); err == nil {
		t.Fatal("missing input accepted")
	}
}
