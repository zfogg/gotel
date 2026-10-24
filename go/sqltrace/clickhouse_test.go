package sqltrace

import (
	"strings"
	"testing"
)

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
