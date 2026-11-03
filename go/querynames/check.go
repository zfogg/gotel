package querynames

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"regexp"
	"slices"

	sequel "github.com/zfogg/sequel/go"
	"golang.org/x/tools/go/packages"
)

var identifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var pascalName = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// Check type-checks the selected packages and returns the number of execution
// sites and actionable errors. No SQL or bind values appear in errors. Package
// load/type errors are included; incomplete analysis never silently passes.
func Check(root string, configs ...Config) (int, []error) {
	config := DefaultConfig()
	if len(configs) > 0 {
		config = configs[0]
	}
	return config.Check(root)
}

// Check applies this project's source naming policy.
func (config Config) Check(root string) (int, []error) {
	if config.DefinitionIdentity != "" && config.DefinitionIdentity != DefinitionExact && config.DefinitionIdentity != DefinitionShape {
		return 0, []error{fmt.Errorf("unknown static SQL definition identity policy")}
	}
	c := &checker{config: config}
	patterns := config.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	pkgs, err := packages.Load(&packages.Config{Dir: root, Context: config.Context, Mode: packages.LoadSyntax}, patterns...)
	if err != nil {
		return 0, []error{err}
	}
	return c.checkPackages(pkgs)
}

func (c *checker) checkPackages(pkgs []*packages.Package) (int, []error) {
	config := c.config
	var errors []error
	seen, definitions := map[string]string{}, map[string]string{}
	count := 0
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			// Compiler prose may quote a SQL literal that failed type checking.
			errors = append(errors, fmt.Errorf("%s: Go package failed to load or type-check; run go test for compiler details", e.Pos))
		}
		for _, file := range pkg.Syntax {
			path := pkg.Fset.Position(file.Pos()).Filename
			if matchPath(path, config.Exclude) {
				continue
			}
			definitionFile := matchPath(path, config.DefinitionFiles)
			if !definitionFile {
				errors = append(errors, c.checkCallerScopes(file, pkg.TypesInfo, pkg.Fset)...)
			}
			assignments := literalAssignments(file, pkg.TypesInfo)
			owners := map[*ast.CallExpr]*ast.FuncDecl{}
			for _, decl := range file.Decls {
				if function, ok := decl.(*ast.FuncDecl); ok && function.Body != nil {
					ast.Inspect(function.Body, func(n ast.Node) bool {
						if call, ok := n.(*ast.CallExpr); ok {
							owners[call] = function
						}
						return true
					})
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				var rule *Receiver
				for i := range config.Receivers {
					r := &config.Receivers[i]
					if matchesType(pkg.TypesInfo.TypeOf(sel.X), r.Type) && slices.Contains(r.Methods, sel.Sel.Name) {
						rule = r
						break
					}
				}
				if rule == nil {
					return true
				}
				if config.AllowCall != nil && config.AllowCall(owners[call], call, pkg.TypesInfo) {
					return true
				}
				if sel.Sel.Name != "Raw" && sel.Sel.Name != "Exec" && hasRaw(sel.X) {
					return true
				}
				count++
				where := pkg.Fset.Position(call.Pos()).String()
				if config.RequireDefinitions && !definitionFile {
					errors = append(errors, fmt.Errorf("%s: execute SQL through a generated named query method", where))
				}
				names := c.explicitNames(sel.X, pkg.TypesInfo, assignments)
				if rule.ContextArgument >= 0 && rule.ContextArgument < len(call.Args) {
					names = c.explicitNames(call.Args[rule.ContextArgument], pkg.TypesInfo, assignments)
				}
				if len(names) == 0 && owners[call] != nil {
					if fn, ok := pkg.TypesInfo.Defs[owners[call].Name].(*types.Func); ok {
						sig, _ := fn.Type().(*types.Signature)
						if sig != nil && sig.Recv() != nil {
							for _, receiver := range config.CallerTypes {
								if matchesType(sig.Recv().Type(), receiver) {
									names = []string{fn.Name()}
									break
								}
							}
						}
					}
				}
				definition := c.queryDefinition(call, pkg.TypesInfo)
				if rule.SQLArgument >= 0 && rule.SQLArgument < len(call.Args) {
					if sql, ok := constantString(call.Args[rule.SQLArgument], pkg.TypesInfo); ok {
						shape := c.queryIdentity(sql)
						if shape == "" {
							errors = append(errors, fmt.Errorf("%s: SQL identity unavailable; manual review required", where))
						} else {
							definition = "sql:" + shape
						}
						if len(names) == 0 {
							if name := sequel.SQLCName(sql); name != "" {
								names = []string{name}
							}
						}
					} else if config.ReportDynamicSQL {
						errors = append(errors, fmt.Errorf("%s: dynamic SQL identity requires manual review", where))
					}
				}
				if len(names) == 0 {
					errors = append(errors, fmt.Errorf("%s: %s needs a source-owned static query name; dynamic names require manual review", where, sel.Sel.Name))
				}
				for _, name := range names {
					valid := identifier.MatchString(name)
					if config.PascalCase {
						valid = pascalName.MatchString(name)
					}
					if !valid {
						errors = append(errors, fmt.Errorf("%s: invalid source-owned query name; expected a static identifier", where))
						continue
					}
					if prior, ok := seen[name]; ok && prior != where {
						errors = append(errors, fmt.Errorf("%s: query name %s already used at %s", where, name, prior))
					} else {
						seen[name] = where
					}
					if definition != "" {
						if prior, ok := definitions[definition]; ok && prior != name {
							errors = append(errors, fmt.Errorf("%s: query %s duplicates the definition of %s; call its shared named method", where, name, prior))
						} else {
							definitions[definition] = name
						}
					}
				}
				return true
			})
		}
	}
	return count, errors
}

func constantString(expr ast.Expr, info *types.Info) (string, bool) {
	if v := info.Types[expr].Value; v != nil && v.Kind() == constant.String {
		return constant.StringVal(v), true
	}
	return "", false
}

func hasRaw(e ast.Expr) bool {
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if sel.Sel.Name == "Raw" {
			return true
		}
		e = sel.X
	}
}

func (c *checker) namingFunction(call *ast.CallExpr, info *types.Info) *NamingFunction {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		id = fn
	case *ast.SelectorExpr:
		id = fn.Sel
	}
	if id == nil {
		return nil
	}
	obj := info.Uses[id]
	if obj == nil || obj.Pkg() == nil {
		return nil
	}
	for i := range c.config.NamingFunctions {
		rule := &c.config.NamingFunctions[i]
		if rule.Name == obj.Name() && rule.Package == obj.Pkg().Path() {
			return rule
		}
	}
	return nil
}

func (c *checker) explicitNames(e ast.Expr, info *types.Info, assignments map[types.Object][]ast.Expr) []string {
	return c.names(e, info, assignments, map[types.Object]bool{})
}

func (c *checker) names(e ast.Expr, info *types.Info, assignments map[types.Object][]ast.Expr, seen map[types.Object]bool) []string {
	if id, ok := e.(*ast.Ident); ok {
		obj := info.Uses[id]
		if obj == nil || seen[obj] {
			return nil
		}
		seen[obj] = true
		var names []string
		for _, value := range assignments[obj] {
			found := c.names(value, info, assignments, seen)
			if len(found) == 0 {
				return nil
			}
			names = append(names, found...)
		}
		return names
	}
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return nil
		}
		if rule := c.namingFunction(call, info); rule != nil && rule.NameArgument >= 0 && rule.NameArgument < len(call.Args) {
			arg := call.Args[rule.NameArgument]
			if name, ok := constantString(arg, info); ok {
				return []string{name}
			}
			if id, ok := arg.(*ast.Ident); ok {
				var names []string
				for _, value := range assignments[info.Uses[id]] {
					name, ok := constantString(value, info)
					if !ok {
						return nil
					}
					names = append(names, name)
				}
				return names
			}
			return nil
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if sel.Sel.Name == "WithContext" {
				if len(call.Args) == 1 {
					return c.names(call.Args[0], info, assignments, seen)
				}
				return nil
			}
			e = sel.X
		} else {
			return nil
		}
	}
}

func literalAssignments(file *ast.File, info *types.Info) map[types.Object][]ast.Expr {
	result := map[types.Object][]ast.Expr{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					obj := info.Defs[id]
					if obj == nil {
						obj = info.Uses[id]
					}
					var value ast.Expr
					if len(n.Rhs) == len(n.Lhs) {
						value = n.Rhs[i]
					}
					result[obj] = append(result[obj], value)
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				var value ast.Expr
				if len(n.Values) == len(n.Names) {
					value = n.Values[i]
				}
				result[info.Defs[id]] = append(result[info.Defs[id]], value)
			}
		}
		return true
	})
	return result
}
