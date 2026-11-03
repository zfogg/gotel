package querynames

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

type scopeTestImporter struct{ pkg *types.Package }

func (i scopeTestImporter) Import(path string) (*types.Package, error) {
	if path == i.pkg.Path() {
		return i.pkg, nil
	}
	return nil, fmt.Errorf("unexpected import %s", path)
}
func TestCallerScopesFollowTypedAliases(t *testing.T) {
	gorm := types.NewPackage("gorm.io/gorm", "gorm")
	name := types.NewTypeName(token.NoPos, gorm, "DB", nil)
	db := types.NewNamed(name, types.NewStruct(nil, nil), nil)
	gorm.Scope().Insert(name)
	ptr := types.NewPointer(db)
	for _, method := range []string{"Where", "Session"} {
		var params *types.Tuple
		if method == "Where" {
			params = types.NewTuple(types.NewVar(token.NoPos, gorm, "sql", types.Typ[types.String]))
		} else {
			params = types.NewTuple()
		}
		sig := types.NewSignatureType(types.NewVar(token.NoPos, gorm, "db", ptr), nil, nil, params, types.NewTuple(types.NewVar(token.NoPos, gorm, "", ptr)), false)
		db.AddMethod(types.NewFunc(token.NoPos, gorm, method, sig))
	}
	gorm.MarkComplete()
	for _, tc := range []struct {
		name, body string
		bad        bool
	}{
		{"direct", `storageQueries.Read(db.Where("id=?"))`, true},
		{"alias", `q:=db.Where("id=?");var next=q;storageQueries.Read(next)`, true},
		{"reassignment", `q:=db;q=q.Where("id=?");storageQueries.Read(q)`, true},
		{"session", `storageQueries.Read(db.Session())`, false},
		{"base", `storageQueries.Read(db)`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := token.NewFileSet()
			f, err := parser.ParseFile(fs, "caller.go", `package example;import "gorm.io/gorm";type storageQueryCatalog struct{};var storageQueries storageQueryCatalog;func(storageQueryCatalog)Read(db *gorm.DB){};func run(db *gorm.DB){`+tc.body+`}`, 0)
			if err != nil {
				t.Fatal(err)
			}
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
			conf := types.Config{Importer: scopeTestImporter{gorm}}
			if _, err := conf.Check("example", fs, []*ast.File{f}, info); err != nil {
				t.Fatal(err)
			}
			config := DefaultConfig()
			config.CatalogTypes = []Type{{"example", "storageQueryCatalog"}}
			c := &checker{config: config}
			errors := c.checkCallerScopes(f, info, fs)
			if (len(errors) > 0) != tc.bad {
				t.Fatalf("errors=%v wantbad=%v", errors, tc.bad)
			}
		})
	}
}
