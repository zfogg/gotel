package querynames

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func TestUnchangedForwarder(t *testing.T) {
	for _, test := range []struct {
		name, body string
		allowed    bool
	}{
		{"direct", `return e.DBTX.Exec(ctx,query,args...)`, true},
		{"wrapped context", `return e.DBTX.Exec(suppress(ctx),query,args...)`, true},
		{"mutable instrumentation state", `e.rows[kind]++; return e.DBTX.Exec(ctx,query,args...)`, true},
		{"instrumentation field assignment", `e.rows = counts; e.span = span; return e.DBTX.Exec(ctx,query,args...)`, true},
		{"instrumentation field helper", `observe(e.span); e.span.SetAttributes(attrs...); return e.DBTX.Exec(ctx,query,args...)`, true},
		{"instrumentation field alias", `span := e.span; span.End(); return e.DBTX.Exec(ctx,query,args...)`, true},
		{"instrumentation field pointer", `observe(&e.rows); return e.DBTX.Exec(ctx,query,args...)`, true},
		{"assigned driver result", `tag,err := e.DBTX.Exec(ctx,query,args...); return tag,err`, true},
		{"literal SQL", `return e.DBTX.Exec(ctx,"SELECT 1",args...)`, false},
		{"query reassignment", `query += " WHERE true"; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"query shadow", `if true { query := "SELECT 1"; return e.DBTX.Exec(ctx,query,args...) }; return nil`, false},
		{"query declaration", `if true { var query = "SELECT 1"; return e.DBTX.Exec(ctx,query,args...) }; return nil`, false},
		{"query pointer escape", `rewrite(&query); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"closure shadow", `return func(query string) any { return e.DBTX.Exec(ctx,query,args...) }("SELECT 1")`, false},
		{"receiver shadow", `e := other; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"receiver pointer escape", `rewrite(&e); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"receiver object escape", `rewrite(e); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"delegate assignment", `e.DBTX = other; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"delegate pointer escape", `rewrite(&e.DBTX); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"changed arguments", `return e.DBTX.Exec(ctx,query,otherArgs...)`, false},
		{"argument assignment", `args = otherArgs; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument mutation", `args[0] = "secret"; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument escape", `rewrite(args); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument alias", `alias := args; alias[0] = 99; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument declared alias", `var alias = args; alias[0] = 99; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument reslice alias", `alias := (args)[:]; alias[0] = 99; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument composite alias", `alias := struct{values []any}{args}; alias.values[0] = 99; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument slice escape", `rewrite(args[:]); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument nested slice escape", `rewrite(any(args[:len(args)])); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument pointer escape", `rewrite(&args[0]); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument derived mutation", `(args[:])[0] = 99; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument pointer mutation", `(*(&args))[0] = 99; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument pointer element mutation", `*(&args[0]) = 99; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument return escape", `e.DBTX.Exec(ctx,query,args...); return args[:]`, false},
		{"argument closure return escape", `alias := func()[]any{return args}; rewrite(alias()); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument channel escape", `channel <- args[:]; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"argument deferred escape", `defer rewrite(args[:]); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"receiver alias", `alias := e; alias.DBTX = other; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"receiver declared alias", `var alias = e; alias.DBTX = other; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"receiver converted alias", `alias := any(e); rewrite(alias); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"receiver pointer alias", `alias := &e; (*alias).DBTX = other; return e.DBTX.Exec(ctx,query,args...)`, false},
		{"receiver derived escape", `rewrite(any(e)); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"delegate alias", `alias := e.DBTX; rewrite(alias); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"delegate method alias", `alias := e.DBTX.Exec; alias(ctx,query,args...); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"delegate derived escape", `rewrite(any(e.DBTX)); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"context argument escape", `return e.DBTX.Exec(rewrite(args[:]),query,args...)`, false},
		{"argument closure mutation", `func(){ args[0] = "secret" }(); return e.DBTX.Exec(ctx,query,args...)`, false},
		{"no ellipsis", `return e.DBTX.Exec(ctx,query,args)`, false},
		{"wrong method", `return e.DBTX.Query(ctx,query,args...)`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture\nfunc(e *executor) Exec(ctx context.Context,query string,args ...any) any {"+test.body+"}", 0)
			if err != nil {
				t.Fatal(err)
			}
			function := file.Decls[0].(*ast.FuncDecl)
			var selected *ast.CallExpr
			ast.Inspect(function, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if method, ok := call.Fun.(*ast.SelectorExpr); ok && (method.Sel.Name == "Exec" || method.Sel.Name == "Query") {
						selected = call
					}
				}
				return true
			})
			if allowed := UnchangedForwarder(function, selected, nil); allowed != test.allowed {
				t.Fatalf("allowed=%v, want %v", allowed, test.allowed)
			}
		})
	}
}

func TestUnchangedForwarderUsesTypeIdentity(t *testing.T) {
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, "fixture.go", `package fixture
type DBTX interface { Exec(any,string,...any) any }
type executor struct { DBTX }
func(e *executor) Exec(ctx any,query string,args ...any) any {
 return e.DBTX.Exec(ctx,query,args...)
}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	if _, err := new(types.Config).Check("fixture", positions, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	function := file.Decls[2].(*ast.FuncDecl)
	call := function.Body.List[0].(*ast.ReturnStmt).Results[0].(*ast.CallExpr)
	if !UnchangedForwarder(function, call, info) {
		t.Fatal("typed transparent forwarding rejected")
	}
	query := call.Args[1].(*ast.Ident)
	delete(info.Uses, query)
	if UnchangedForwarder(function, call, info) {
		t.Fatal("missing identifier identity accepted")
	}
}
