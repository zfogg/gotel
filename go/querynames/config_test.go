package querynames

import (
	"go/token"
	"go/types"
	"testing"
)

func TestImportGlobsUseSlashSemanticsOnEveryPlatform(t *testing.T) {
	pkg := types.NewPackage("example/internal/nested/storage", "storage")
	named := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "DB", nil), types.NewStruct(nil, nil), nil)
	if matchesType(named, Type{"example/internal/*", "DB"}) {
		t.Fatal("one-level glob matched a nested package")
	}
	if !matchesType(named, Type{"example/internal/**", "DB"}) {
		t.Fatal("recursive glob did not match nested package")
	}
	if matchesType(named, Type{"example/internal/**", "Other"}) {
		t.Fatal("receiver type constraint lost")
	}
	if !matchPath("/repo/internal/storage/queries.gen.go", []string{"*/queries.gen.go"}) {
		t.Fatal("file suffix glob failed")
	}
	if matchPath("/repo/internal/storage/nested/queries.gen.go", []string{"storage/queries.gen.go"}) {
		t.Fatal("suffix glob crossed a directory")
	}
}
