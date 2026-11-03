package querynames

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestTypedExecutionAdapters(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		caller           bool
		count            int
	}{
		{"sqlc constant", `const sql="-- name: ReadWidget :one\nSELECT id FROM widgets WHERE id=$1";db.Query(Context{},sql,1)`, "", false, 1},
		{"typed context alias", `ctx:=Name(Context{},"ReadWidget");db.Query(ctx,"SELECT id FROM widgets")`, "", false, 1},
		{"runtime context name", `ctx:=Name(Context{},runtimeName);db.Query(ctx,"SELECT id FROM widgets")`, "static query name", false, 1},
		{"invalid name redaction", `ctx:=Name(Context{},"SELECT 'private-value'");db.Query(ctx,"SELECT id FROM widgets")`, "invalid source-owned", false, 1},
		{"dynamic SQL", `ctx:=Name(Context{},"ReadWidget");db.Query(ctx,runtimeSQL)`, "dynamic SQL identity", false, 1},
		{"caller owned", `db.Query(Context{},"SELECT id FROM widgets")`, "", true, 1},
		{"decoy context argument", `db.Query(Context{},"SELECT id FROM widgets",Name(Context{},"Decoy"))`, "static query name", false, 1},
		{"other receiver", `other.Query(Context{},"SELECT id FROM widgets")`, "", false, 0},
		{"same body distinct SQLC names", `const one="-- name: One :one\nSELECT id FROM widgets WHERE key=$1";const two="-- name: Two :one\nSELECT id FROM widgets WHERE key=$1";db.Query(Context{},one);db.Query(Context{},two)`, "duplicates the definition", false, 2},
		{"distinct fixed literal semantics", `db.Query(Name(Context{},"Fill"),"SELECT id FROM widgets WHERE key='fill'");db.Query(Name(Context{},"Alert"),"SELECT id FROM widgets WHERE key=$1")`, "", false, 2},
		{"literal whitespace is semantic", `db.Query(Name(Context{},"One"),"SELECT 'two spaces'");db.Query(Name(Context{},"Two"),"SELECT 'two  spaces'")`, "", false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := token.NewFileSet()
			file, err := parser.ParseFile(fs, "queries.go", `package fixture
type Context struct{};func Name(ctx Context,name string)Context{return ctx}
type DB struct{};func(*DB)Query(Context,string,...any){}
type Other struct{};func(*Other)Query(Context,string,...any){}
type Owner struct{}
func(*Owner)ReadWidget(db *DB,other *Other,runtimeName,runtimeSQL string){`+tc.body+`}`, 0)
			if err != nil {
				t.Fatal(err)
			}
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
			pkg, err := (&types.Config{}).Check("example/fixture", fs, []*ast.File{file}, info)
			if err != nil {
				t.Fatal(err)
			}
			config := Config{Receivers: []Receiver{{Type: Type{"example/fixture", "DB"}, Methods: []string{"Query"}, SQLArgument: 1, ContextArgument: 0}}, NamingFunctions: []NamingFunction{{"example/fixture", "Name", 1, 0, true}}, ReportDynamicSQL: true}
			if tc.caller {
				config.CallerTypes = []Type{{"example/fixture", "Owner"}}
			}
			count, errors := (&checker{config: config}).checkPackages([]*packages.Package{{Fset: fs, Syntax: []*ast.File{file}, TypesInfo: info, Types: pkg}})
			if count != tc.count {
				t.Fatalf("count=%d errors=%v", count, errors)
			}
			if tc.want == "" {
				if len(errors) > 0 {
					t.Fatal(errors)
				}
				return
			}
			for _, err := range errors {
				if strings.Contains(err.Error(), "private-value") {
					t.Fatal("invalid name leaked into a diagnostic")
				}
				if strings.Contains(err.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("expected %q, got %v", tc.want, errors)
		})
	}
}
