package gotel

import "testing"

func TestStorageNamesDoNotIncludeApplication(t *testing.T) {
	app := NewApplication("beanaries")
	for _, tc := range []struct{ system, operation, want string }{
		{"postgresql", "ListBuilds", "storage.postgres.ListBuilds"},
		{"sqlite", "ListBuilds", "storage.sqlite.ListBuilds"},
		{"redis", "get", "storage.redis.GET"},
	} {
		if got := app.StorageName(tc.system, tc.operation); got != tc.want {
			t.Errorf("name = %q, want %q", got, tc.want)
		}
	}
}
