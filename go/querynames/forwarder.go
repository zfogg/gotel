package querynames

import (
	"go/ast"
	"go/token"
	"go/types"
)

// UnchangedForwarder proves the narrow shape of a transparent SQL executor
// decorator: its method forwards (context, SQL, args...) to one receiver field.
// Context may be wrapped; SQL, argument slice and delegate must remain unchanged
// throughout the method, including nested closures. It conservatively rejects
// reassignment, shadowing, mutation, aliases and derived mutable-input escape.
// Writes to unrelated receiver fields remain valid instrumentation. The caller must
// additionally restrict the allowed decorator receiver type and delegate field.
// info may be nil for syntax-only fixture checks; typed callers also verify
// identifier object identity.
func UnchangedForwarder(function *ast.FuncDecl, call *ast.CallExpr, info *types.Info) bool {
	if function == nil || function.Body == nil || function.Recv == nil || len(function.Recv.List) != 1 || len(function.Recv.List[0].Names) != 1 || call == nil {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || function.Name.Name != selector.Sel.Name {
		return false
	}
	parameters := function.Type.Params.List
	if len(parameters) != 3 || len(parameters[1].Names) != 1 || len(parameters[2].Names) != 1 || len(call.Args) != 3 || !call.Ellipsis.IsValid() {
		return false
	}
	queryType, ok := parameters[1].Type.(*ast.Ident)
	if !ok || queryType.Name != "string" {
		return false
	}
	if _, ok := parameters[2].Type.(*ast.Ellipsis); !ok {
		return false
	}
	delegate, ok := selector.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := delegate.X.(*ast.Ident)
	if !ok {
		return false
	}
	query, queryOK := call.Args[1].(*ast.Ident)
	args, argsOK := call.Args[2].(*ast.Ident)
	if !queryOK || !argsOK {
		return false
	}
	same := func(use, definition *ast.Ident) bool {
		if use.Name != definition.Name {
			return false
		}
		return info == nil || info.Uses[use] != nil && info.Uses[use] == info.Defs[definition]
	}
	if !same(receiver, function.Recv.List[0].Names[0]) || !same(query, parameters[1].Names[0]) || !same(args, parameters[2].Names[0]) {
		return false
	}
	protected := map[string]bool{receiver.Name: true, query.Name: true, args.Name: true}
	mutable := map[string]bool{receiver.Name: true, args.Name: true}
	// Driver calls may consume the original argument slice and return results;
	// that is the one allowed mutable-input transfer. Each SQL call is checked
	// separately, so this recognition does not exempt a neighboring inline query.
	forwardingCall := func(value *ast.CallExpr) bool {
		selected, ok := value.Fun.(*ast.SelectorExpr)
		if !ok || selected.Sel.Name != function.Name.Name || len(value.Args) != 3 || !value.Ellipsis.IsValid() {
			return false
		}
		field, ok := selected.X.(*ast.SelectorExpr)
		if !ok || field.Sel.Name != delegate.Sel.Name {
			return false
		}
		id, ok := field.X.(*ast.Ident)
		if !ok || !same(id, function.Recv.List[0].Names[0]) {
			return false
		}
		arg, ok := value.Args[2].(*ast.Ident)
		return ok && same(arg, parameters[2].Names[0])
	}
	// Reject aliases at their creation instead of trying to track subsequent
	// mutation through arbitrary alias chains. Stop at unrelated receiver fields
	// so e.rows/e.span instrumentation can be read, written and passed to helpers.
	borrowsMutable := func(expression ast.Expr) bool {
		found := false
		ast.Inspect(expression, func(node ast.Node) bool {
			if found {
				return false
			}
			switch value := node.(type) {
			case *ast.CallExpr:
				if forwardingCall(value) {
					return false
				}
			case *ast.SelectorExpr:
				if id, ok := value.X.(*ast.Ident); ok && id.Name == receiver.Name {
					found = value.Sel.Name == delegate.Sel.Name
					return false
				}
			case *ast.Ident:
				found = mutable[value.Name]
			}
			return !found
		})
		return found
	}
	// A direct delegate assignment and any argument-slice element mutation alter
	// the call even though the original variable identifier is still forwarded.
	var changes func(ast.Expr) bool
	changes = func(expression ast.Expr) bool {
		switch value := expression.(type) {
		case *ast.Ident:
			return protected[value.Name]
		case *ast.ParenExpr:
			return changes(value.X)
		case *ast.IndexExpr:
			return borrowsMutable(value.X)
		case *ast.SelectorExpr:
			if id, ok := value.X.(*ast.Ident); ok && id.Name == receiver.Name {
				return value.Sel.Name == delegate.Sel.Name
			}
			return borrowsMutable(value.X)
		case *ast.StarExpr:
			return borrowsMutable(value.X)
		}
		return false
	}
	unchanged := true
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if !unchanged {
			return false
		}
		switch value := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range value.Lhs {
				unchanged = unchanged && !changes(lhs)
			}
			for _, rhs := range value.Rhs {
				unchanged = unchanged && !borrowsMutable(rhs)
			}
		case *ast.IncDecStmt:
			unchanged = !changes(value.X)
		case *ast.ValueSpec:
			for _, name := range value.Names {
				unchanged = unchanged && !protected[name.Name]
			}
			for _, expression := range value.Values {
				unchanged = unchanged && !borrowsMutable(expression)
			}
		case *ast.Field:
			for _, name := range value.Names {
				unchanged = unchanged && !protected[name.Name]
			}
		case *ast.RangeStmt:
			unchanged = !changes(value.Key) && !changes(value.Value) && !borrowsMutable(value.X)
		case *ast.ReturnStmt:
			for _, expression := range value.Results {
				unchanged = unchanged && !borrowsMutable(expression)
			}
		case *ast.SendStmt:
			unchanged = !borrowsMutable(value.Value)
		case *ast.UnaryExpr:
			if value.Op == token.AND {
				unchanged = !changes(value.X)
			}
		case *ast.CallExpr:
			// Passing mutable inputs to an unknown function may mutate the
			// delegate or argument slice, including through derived expressions.
			forwarding := forwardingCall(value)
			if !forwarding && borrowsMutable(value.Fun) {
				unchanged = false
			}
			for index, arg := range value.Args {
				if forwarding && index == 2 {
					continue
				}
				if borrowsMutable(arg) {
					unchanged = false
				}
			}
		}
		return unchanged
	})
	return unchanged
}
