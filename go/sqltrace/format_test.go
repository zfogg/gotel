package sqltrace

import (
	"strings"
	"testing"
)

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
