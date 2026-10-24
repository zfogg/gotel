package sqltrace

import "testing"

func TestShortScalarTelemetryPresentation(t *testing.T) {
	for _, dialect := range []Dialect{DialectGeneric, DialectSQLite, DialectPostgreSQL, DialectDuckDB, DialectMySQL, DialectClickHouse} {
		for _, query := range []string{"SELECT 1", "SELECT 'private-value'", "SELECT 987654"} {
			meta := InspectDialect(MustNamer("app.storage"), "Health", query, nil, dialect)
			if meta.Summary != "SELECT ?" || meta.SanitizedSQL != "SELECT ?" {
				t.Fatalf("dialect %v query %q: %+v", dialect, query, meta)
			}
			found := false
			for _, attr := range meta.Attributes("clickhouse", "test") {
				if attr.Key == QueryTextKey {
					found = true
					if attr.Value.AsString() != "SELECT ?" {
						t.Fatalf("query text = %q", attr.Value.AsString())
					}
				}
			}
			if !found {
				t.Fatal("missing query text")
			}
		}
	}
}
