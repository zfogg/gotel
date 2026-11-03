package querynames

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

var sqlScopeMethods = map[string]bool{}

func init() {
	for _, name := range []string{"Where", "Not", "Or", "Order", "Select", "Distinct", "Table", "Model", "Joins", "InnerJoins", "Group", "Having", "Limit", "Offset", "Scopes", "Clauses", "Unscoped", "Preload"} {
		sqlScopeMethods[name] = true
	}
}

// Follow typed local aliases, including reassignment, so callers cannot conceal
// SQL construction behind q := db.Where(...); catalog.Read(q). Transaction and
// context/session propagation remain valid inputs to a query definition.
func (ck *checker) checkCallerScopes(file *ast.File, info *types.Info, fs *token.FileSet) []error {
	aliases := map[types.Object][]ast.Expr{}
	object := func(id *ast.Ident) types.Object {
		if obj := info.Defs[id]; obj != nil {
			return obj
		}
		return info.Uses[id]
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && i < len(x.Rhs) {
					obj := object(id)
					aliases[obj] = append(aliases[obj], x.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if i < len(x.Values) {
					obj := object(id)
					aliases[obj] = append(aliases[obj], x.Values[i])
				}
			}
		}
		return true
	})
	var scoped func(ast.Expr, map[types.Object]bool) bool
	scoped = func(expr ast.Expr, seen map[types.Object]bool) bool {
		found := false
		ast.Inspect(expr, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sqlScopeMethods[sel.Sel.Name] && ck.databaseType(info.TypeOf(sel.X)) {
					found = true
					return false
				}
			case *ast.Ident:
				obj := object(x)
				if obj != nil && !seen[obj] {
					seen[obj] = true
					for _, value := range aliases[obj] {
						if scoped(value, seen) {
							found = true
							return false
						}
					}
				}
			}
			return !found
		})
		return found
	}
	var errors []error
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		typ := info.TypeOf(sel.X)
		if p, ok := typ.(*types.Pointer); ok {
			typ = p.Elem()
		}
		catalog, ok := typ.(*types.Named)
		if !ok || !ck.catalogType(catalog) {
			return true
		}
		for _, arg := range call.Args {
			if ck.databaseType(info.TypeOf(arg)) && scoped(arg, map[types.Object]bool{}) {
				errors = append(errors, fmt.Errorf("%s: caller-built SQL scopes must be owned by the generated query definition", fs.Position(call.Pos())))
				break
			}
		}
		return true
	})
	return errors
}
