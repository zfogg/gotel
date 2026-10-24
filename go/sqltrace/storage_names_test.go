package sqltrace

import "testing"

func TestStorageNamespaces(t *testing.T) {
	for _, tc := range []struct{ system, want string }{
		{"postgresql", "storage.postgres.ListBuilds"},
		{"postgres", "storage.postgres.ListBuilds"},
		{"sqlite", "storage.sqlite.ListBuilds"},
		{"duckdb", "storage.duckdb.ListBuilds"},
		{"clickhouse", "storage.clickhouse.ListBuilds"},
	} {
		t.Run(tc.system, func(t *testing.T) {
			if got := StorageNamer(tc.system).Name("ListBuilds"); got != tc.want {
				t.Fatalf("name = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStorageNamespaceRejectsDynamicComponents(t *testing.T) {
	for _, system := range []string{"", "postgres.tenant", "sql customer", "1postgres"} {
		t.Run(system, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid system accepted")
				}
			}()
			StorageNamer(system)
		})
	}
}
