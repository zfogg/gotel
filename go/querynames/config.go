// Package querynames checks application-owned SQL naming boundaries using Go
// type information. Project policy is configuration, not embedded package paths.
package querynames

import (
	"context"
	"go/ast"
	"go/types"
	"path"
	"path/filepath"
	"strings"

	sequel "github.com/zfogg/sequel/go"
)

// Type identifies a named receiver; an empty Name matches any type in Package.
// Package accepts slash-separated glob patterns; ** matches arbitrary suffixes.
type Type struct {
	// Package is an import path or glob. A trailing /** includes nested packages.
	Package string
	// Name is the named receiver type; empty matches every named type in Package.
	Name string
}

// Receiver lists typed database execution methods and argument indexes.
type Receiver struct {
	Type
	// Methods are exact execution names; an empty list matches none.
	Methods []string
	// SQLArgument indexes the SQL string; negative selects ORM identity rules.
	SQLArgument int
	// ContextArgument indexes the naming context; negative uses the DB chain.
	ContextArgument int
}

// NamingFunction identifies a type-resolved wrapper or context naming helper.
type NamingFunction struct {
	// Package is the exact import path owning the helper.
	Package string
	// Name is the function or method identifier.
	Name string
	// NameArgument is the zero-based static operation-name argument.
	NameArgument int
	// ReceiverArgument is the zero-based wrapped database/context argument.
	ReceiverArgument int
	// Context marks context helpers; false marks wrappers returning a database.
	Context bool
}

// Config describes the project's naming boundary. Patterns defaults to ./....
// Exclude and DefinitionFiles match slash-normalized path suffixes using glob
// patterns (for example */storage_queries.gen.go). Exclude also accepts a
// directory fragment ending in /. Tests and generated source are not implicitly
// excluded: projects must state which files belong to the boundary.
type Config struct {
	// Dialect selects static SQL fingerprint rules; empty uses PostgreSQL.
	Dialect sequel.Dialect
	// Context cancels package loading and compiler subprocesses; nil is unbounded.
	Context context.Context
	// Patterns are go/packages patterns, defaulting to ./....
	Patterns []string
	// Exclude lists explicit file globs or slash-terminated directory fragments.
	Exclude []string
	// DefinitionFiles lists files allowed to own execution under RequireDefinitions.
	DefinitionFiles []string
	// Receivers defines typed execution boundaries; empty checks no executors.
	Receivers []Receiver
	// NamingFunctions defines source-owned name/context helper APIs.
	NamingFunctions []NamingFunction
	// CatalogTypes identifies catalogs whose callers cannot supply SQL scopes.
	CatalogTypes []Type
	// CallerTypes permits method names owned by runtime CallerNamer receivers.
	CallerTypes []Type
	// RequireDefinitions rejects executions outside DefinitionFiles; default false.
	RequireDefinitions bool
	// PascalCase requires [A-Z][A-Za-z0-9]* instead of identifier syntax.
	PascalCase bool
	// ReportDynamicSQL requires manual review of nonconstant SQL identities.
	ReportDynamicSQL bool
	// AllowCall admits only a verified transparent forwarding call. The hook
	// receives its enclosing declaration and type information. Restrict receiver
	// ownership explicitly and fail closed when proof is unavailable.
	AllowCall func(*ast.FuncDecl, *ast.CallExpr, *types.Info) bool
}

// DefaultConfig recognizes database/sql, pgx, GORM and Gotel's context helper.
// Applications using their own wrapper should add its typed NamingFunction.
func DefaultConfig() Config {
	return Config{
		Patterns: []string{"./..."}, Exclude: []string{"*_test.go"},
		Receivers: []Receiver{
			{Type{"gorm.io/gorm", "DB"}, strings.Fields("Raw Exec Find First Take Last Scan Count Pluck Create CreateInBatches Save Update Updates UpdateColumn UpdateColumns Delete FirstOrCreate FirstOrInit Rows Row FindInBatches"), -1, -1},
			{Type{"database/sql", ""}, []string{"ExecContext", "QueryContext", "QueryRowContext"}, 1, 0},
			{Type{"database/sql", ""}, []string{"Exec", "Query", "QueryRow"}, 0, -1},
			{Type{"github.com/jackc/pgx/v5", ""}, []string{"Exec", "Query", "QueryRow"}, 1, 0},
			{Type{"github.com/jackc/pgx/v5/pgxpool", "Pool"}, []string{"Exec", "Query", "QueryRow"}, 1, 0},
		},
		NamingFunctions: []NamingFunction{{"github.com/zfogg/gotel/go/tracers/sql", "WithName", 1, 0, true}},
	}
}

type checker struct{ config Config }

func namedType(t types.Type) *types.Named {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	t = types.Unalias(t)
	n, _ := t.(*types.Named)
	return n
}

func matchesType(t types.Type, rule Type) bool {
	n := namedType(t)
	if n == nil || n.Obj().Pkg() == nil || rule.Name != "" && n.Obj().Name() != rule.Name {
		return false
	}
	importPath := n.Obj().Pkg().Path()
	if strings.HasSuffix(rule.Package, "/**") {
		return strings.HasPrefix(importPath, strings.TrimSuffix(rule.Package, "**"))
	}
	ok, _ := path.Match(rule.Package, importPath)
	return ok
}

func (c *checker) databaseType(t types.Type) bool {
	for _, rule := range c.config.Receivers {
		if matchesType(t, rule.Type) {
			return true
		}
	}
	return false
}

func (c *checker) catalogType(t types.Type) bool {
	for _, rule := range c.config.CatalogTypes {
		if matchesType(t, rule) {
			return true
		}
	}
	return false
}

func matchPath(filename string, patterns []string) bool {
	filename = filepath.ToSlash(filename)
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/") && strings.Contains(filename, pattern) {
			return true
		}
		for p := filename; ; {
			if ok, _ := path.Match(pattern, p); ok {
				return true
			}
			i := strings.IndexByte(p, '/')
			if i < 0 {
				break
			}
			p = p[i+1:]
		}
	}
	return false
}
