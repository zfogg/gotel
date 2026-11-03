package querynames

import (
	"go/ast"
	"go/types"
	"strconv"
	"strings"

	sequel "github.com/zfogg/sequel/go"
)

// queryDefinition identifies statically declared SQL and simple GORM model
// queries. Bound values do not change identity. Complex clauses are left to
// their explicit definitions and runtime trace lint rather than guessed.
func (ck *checker) queryDefinition(call *ast.CallExpr, info *types.Info) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if sel.Sel.Name == "Raw" || sel.Sel.Name == "Exec" {
		if len(call.Args) == 0 {
			return ""
		}
		s, ok := queryLiteral(call.Args[0])
		if !ok {
			return ""
		}
		shape := ck.queryShape(s)
		if shape == "" {
			return ""
		}
		return "sql:" + shape
	}
	op := sel.Sel.Name
	if op != "First" && op != "Find" && op != "Count" && op != "Create" {
		return ""
	}
	if len(call.Args) == 0 {
		return ""
	}
	parts, model, ok := ck.queryScope(sel.X, info)
	if !ok {
		return ""
	}
	if model == "" {
		model = queryModel(info.TypeOf(call.Args[0]))
		if op == "Create" {
			t := info.TypeOf(call.Args[0])
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
			}
			if _, ok := t.(*types.Slice); ok {
				model = "batch:" + model
			}
		}
	}
	if model == "" {
		return ""
	}
	if (op == "First" || op == "Find") && len(call.Args) > 1 {
		s, ok := queryLiteral(call.Args[1])
		if !ok {
			return ""
		}
		parts = append(parts, "Where:"+ck.queryShape(s))
	}
	return "gorm:" + op + ":" + model + ":" + strings.Join(parts, "|")
}
func (ck *checker) queryScope(e ast.Expr, info *types.Info) ([]string, string, bool) {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		if ck.databaseType(info.TypeOf(e)) {
			return nil, "", true
		}
		return nil, "", false
	}
	if rule := ck.namingFunction(c, info); rule != nil && !rule.Context && rule.ReceiverArgument >= 0 && rule.ReceiverArgument < len(c.Args) {
		return ck.queryScope(c.Args[rule.ReceiverArgument], info)
	}
	if s, ok := c.Fun.(*ast.SelectorExpr); ok {
		p, m, ok := ck.queryScope(s.X, info)
		if !ok {
			return nil, "", false
		}
		switch s.Sel.Name {
		case "WithContext", "Session", "Debug":
			return p, m, true
		case "Model":
			if len(c.Args) != 1 {
				return nil, "", false
			}
			return p, queryModel(info.TypeOf(c.Args[0])), true
		case "Where", "Order", "Select", "Table":
			if len(c.Args) == 0 {
				return nil, "", false
			}
			v, ok := queryLiteral(c.Args[0])
			if !ok {
				return nil, "", false
			}
			return append(p, s.Sel.Name+":"+ck.queryShape(v)), m, true
		default:
			return nil, "", false
		}
	}
	return nil, "", false
}
func queryLiteral(e ast.Expr) (string, bool) {
	s, ok := e.(*ast.BasicLit)
	if !ok {
		return "", false
	}
	v, err := strconv.Unquote(s.Value)
	return v, err == nil
}
func queryModel(t types.Type) string {
	for {
		switch v := t.(type) {
		case *types.Pointer:
			t = v.Elem()
		case *types.Slice:
			t = v.Elem()
		case *types.Array:
			t = v.Elem()
		default:
			if _, ok := t.(*types.Named); !ok {
				return ""
			}
			return types.TypeString(t, func(p *types.Package) string { return p.Path() })
		}
	}
}
func (ck *checker) queryShape(s string) string {
	dialect := ck.config.Dialect
	if dialect == "" {
		dialect = sequel.DialectPostgreSQL
	}
	q, err := sequel.Fingerprint(dialect, s)
	if err != nil {
		return ""
	}
	return q
}
