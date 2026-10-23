package sqltrace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func TestFormatSQLPreservesContents(t *testing.T) {
	cases := []struct {
		dialect Dialect
		sql     string
		keep    []string
	}{
		{DialectPostgreSQL, "-- name: Find :many\nselect id,payload::jsonb from users where id=$1 and name=$name; -- trailing", []string{"-- name: Find :many", "payload::jsonb", "$1", "$name", "-- trailing"}},
		{DialectPostgreSQL, "select $$a; -- b$$,$tag$x ' y$tag$,E'it\\'s' from t", []string{"$$a; -- b$$", "$tag$x ' y$tag$", "E'it\\'s'"}},
		{DialectMySQL, "select `id`,'a  b',? from users where id=?; # trailing", []string{"`id`", "'a  b'", "?", "# trailing"}},
		{DialectSQLite, "select [name],?1,:name,@name,$scope::name(suffix) from t", []string{"[name]", "?1", ":name", "@name", "$scope::name(suffix)"}},
		{DialectDuckDB, "select * exclude(secret),[1,2,3],{'x':1},9//2 from t qualify row_number() over(partition by team order by id)=1", []string{"exclude(secret)", "[1, 2, 3]", "{'x':1}", "9 // 2", "row_number()"}},
		{DialectPostgreSQL, "/* outer /* nested */ done */ select a,b from t -- don't lose me\nwhere id=$1", []string{"/* outer /* nested */ done */", "-- don't lose me\n", "$1"}},
		{DialectPostgreSQL, "select 'one'\n 'two'", []string{"'one'\n", "'two'"}},
	}
	for _, c := range cases {
		t.Run(string(c.dialect)+c.sql[:6], func(t *testing.T) {
			got, err := FormatSQL(c.dialect, c.sql)
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range c.keep {
				if !strings.Contains(got, fragment) {
					t.Fatalf("lost %q in %s", fragment, got)
				}
			}
			again, err := FormatSQL(c.dialect, got)
			if err != nil || again != got {
				t.Fatalf("unstable: %v\nfirst: %s\nsecond: %s", err, got, again)
			}
		})
	}
}

func TestFormatSQLSharedJSONCorpus(t *testing.T) {
	data, err := os.ReadFile("../testdata/sql_format.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string
			Dialect  Dialect
			SQL      string
			Valid    bool
			Expected string
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) < 600 {
		t.Fatal("formatting corpus unexpectedly small")
	}
	compact := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			out, err := FormatSQL(c.Dialect, c.SQL)
			if !c.Valid {
				if err == nil || out != c.SQL {
					t.Fatal("expected original and error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if compact(out) != compact(c.SQL) {
				t.Fatal("non-whitespace contents changed")
			}
			if c.Expected != "" && out != c.Expected {
				t.Fatalf("layout mismatch\nwant:\n%s\ngot:\n%s", c.Expected, out)
			}
			again, err := FormatSQL(c.Dialect, out)
			if err != nil || again != out {
				t.Fatalf("not idempotent: %v", err)
			}
		})
	}
}

func FuzzFormatSQL(f *testing.F) {
	for _, s := range []string{"select a,b from t where id=$1", "-- trailing  ", "select $$hello$$", "select [1,2]", "select /* nested /* x */ */ ?", "select 'one'\n 'two'"} {
		f.Add(s, uint8(0))
	}
	f.Fuzz(func(t *testing.T, sql string, d uint8) {
		dialect := []Dialect{DialectGeneric, DialectSQLite, DialectPostgreSQL, DialectDuckDB, DialectMySQL}[d%5]
		first, err := FormatSQL(dialect, sql)
		if err != nil {
			if first != sql {
				t.Fatal("error changed input")
			}
			return
		}
		second, err := FormatSQL(dialect, first)
		if err != nil || first != second {
			t.Fatalf("unstable %q -> %q -> %q: %v", sql, first, second, err)
		}
		compact := func(s string) string {
			return strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, s)
		}
		if compact(sql) != compact(first) {
			t.Fatal("changed contents")
		}
	})
}

func TestFormatSQLLayout(t *testing.T) {
	got, err := FormatSQL(DialectDuckDB, "select id,name from users where id=? and active=?")
	want := "select\n  id,\n  name\nfrom users\nwhere\n  id = ?\n  and active = ?"
	if err != nil || got != want {
		t.Fatalf("got %q (%v), want %q", got, err, want)
	}
}

func TestFormatSQLRejectsUnsafeInput(t *testing.T) {
	for _, sql := range []string{"select 'unfinished", "select /* unfinished", "select $$unfinished", "select (id", "select )", "select \x00", "select \xff", strings.Repeat("(", 65) + strings.Repeat(")", 65), strings.Repeat("x", 1024*1024+1)} {
		got, err := FormatSQL(DialectPostgreSQL, sql)
		if err == nil || got != sql {
			t.Fatalf("expected original and error for %.80q", sql)
		}
	}
	if _, err := FormatSQL("typo", "select 1"); err == nil {
		t.Fatal("unknown dialect accepted")
	}
}

func TestFormatSQLCorpus(t *testing.T) {
	paths, err := filepath.Glob("testdata/stonkpile/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			first, err := FormatSQL(DialectPostgreSQL, string(raw))
			if err != nil {
				t.Fatal(err)
			}
			second, err := FormatSQL(DialectPostgreSQL, first)
			if err != nil || second != first {
				t.Fatalf("not stable: %v", err)
			}
		})
	}
}

func TestFormattedTelemetryRemainsSanitized(t *testing.T) {
	m := InspectDialect(Namer{}, "Find", "SELECT id,name FROM accounts WHERE token='SECRET' AND id=$1", []any{"PRIVATE"}, DialectPostgreSQL)
	for _, a := range m.Attributes("postgresql", "") {
		if a.Key == QueryTextKey {
			sql := a.Value.AsString()
			if !strings.Contains(sql, "\n") || strings.Contains(sql, "SECRET") || strings.Contains(sql, "PRIVATE") {
				t.Fatalf("bad telemetry: %s", sql)
			}
		}
	}
}
