package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func invoke(args []string, input string) (string, error) {
	var out, stderr bytes.Buffer
	err := run(args, strings.NewReader(input), &out, &stderr)
	return out.String(), err
}

func TestStdout(t *testing.T) {
	for _, dialect := range []string{"generic", "postgresql", "mysql", "sqlite", "duckdb", "clickhouse"} {
		t.Run(dialect, func(t *testing.T) {
			got, err := invoke([]string{"sql", "format", "--dialect", dialect}, "select a,b from users where id=42;")
			if err != nil || !strings.Contains(got, "a,") || !strings.Contains(got, "b") || !strings.Contains(got, "42") {
				t.Fatalf("%q, %v", got, err)
			}
		})
	}
	got, err := invoke([]string{"sql", "sanitize", "select * from users where name='secret' and id=42"}, "")
	if err != nil || strings.Contains(got, "secret") || strings.Contains(got, "42") || !strings.Contains(got, "?") {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestEditFile(t *testing.T) {
	cases := []struct{ name, dialect, sql, want string }{
		{"multiple", "postgresql", "-- name: First :one\nselect   a, b  from   t; -- trailing\n\n-- Second\nselect   'x;  y', $$a;\n b$$;\n", "-- name: First :one\nselect a, b from t; -- trailing\n\n-- Second\nselect 'x;  y', $$a;\n b$$;\n"},
		{"crlf", "sqlite", "\r\n  select   [a]  from t;\r\n\r\n  select   'b';\r\n", "\r\n  select [a] from t;\r\n\r\n  select 'b';\r\n"},
		{"nested comments", "postgresql", "/* outer\n /* inner */  end */\nselect   1;", "/* outer\n /* inner */  end */\nselect 1;"},
		{"mysql", "mysql", "# comment\nselect   `a`, 'a  b'; # tail\nselect   2;", "# comment\nselect `a`, 'a  b'; # tail\nselect 2;"},
		{"adjacent strings", "postgresql", "select   'a'\n  'b';\n", "select 'a'\n  'b';\n"},
		{"empty", "generic", "", ""},
		{"whitespace", "generic", " \t\n\n", " \t\n\n"},
		{"no final newline", "generic", "select   1;", "select 1;"},
		{"operators", "postgresql", "select  a::text, b->>'x', - -1, x != 2;", "select a::text, b->>'x', - -1, x != 2;"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queries.sql")
			if err := os.WriteFile(path, []byte(c.sql), 0600); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				out, err := invoke([]string{"sql", "format", "--dialect", c.dialect, "--edit-file", path}, "")
				if err != nil || out != "" {
					t.Fatalf("%q, %v", out, err)
				}
				got, err := os.ReadFile(path)
				if err != nil || string(got) != c.want {
					t.Fatalf("got %q want %q: %v", got, c.want, err)
				}
			}
		})
	}
}

func TestFailuresLeaveFileUntouched(t *testing.T) {
	for _, sql := range []string{"select 'unterminated", "/* unfinished", "select $$unfinished", "select \x00", "select \xff", strings.Repeat("x", 1024*1024+1)} {
		path := filepath.Join(t.TempDir(), "bad.sql")
		os.WriteFile(path, []byte(sql), 0600)
		out, err := invoke([]string{"sql", "format", "--dialect", "postgresql", "--edit-file", path}, "")
		if err == nil || out != "" {
			t.Fatalf("expected failure, got %q %v", out, err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != sql {
			t.Fatal("modified input on error")
		}
	}
}

func TestArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"sql", "unknown"}, {"sql", "format", "--dialect", "unknown"},
		{"sql", "sanitize", "--edit-file", "x"}, {"sql", "format", "--edit-file", "missing.sql"},
		{"sql", "format", "a", "b"}, {"sql", "format", "--edit-file", "x", "select 1"},
	} {
		if _, err := invoke(args, "select 1"); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestEditPreservesPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query.sql")
	if err := os.WriteFile(path, []byte("select   1;"), 0640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	if _, err := invoke([]string{"sql", "format", "--edit-file", path}, ""); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if before.Mode().Perm() != after.Mode().Perm() {
		t.Fatalf("mode changed: %v -> %v", before.Mode(), after.Mode())
	}
}

func TestStdoutArgumentAndStdinAgree(t *testing.T) {
	sql := "select a,b from t where x=1; -- tail\nselect 'keep  spaces';"
	for _, command := range []string{"format", "sanitize"} {
		stdin, err := invoke([]string{"sql", command}, sql)
		if err != nil {
			t.Fatal(err)
		}
		arg, err := invoke([]string{"sql", command, sql}, "ignored")
		if err != nil || arg != stdin {
			t.Fatalf("argument differs from stdin: %q, %q, %v", arg, stdin, err)
		}
	}
}
