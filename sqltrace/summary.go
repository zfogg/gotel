package sqltrace

import (
	"context"
	"strconv"
	"strings"

	"github.com/sqlc-dev/darkwing/ast"
	"github.com/sqlc-dev/darkwing/parser"
)

// Dialect selects optional dialect-aware SQL parsing.
type Dialect string

const (
	// DialectGeneric uses the cross-dialect sanitizer and leading SQL operation.
	DialectGeneric Dialect = "generic"
	// DialectPostgreSQL recognizes sqlc names and PostgreSQL placeholders.
	DialectPostgreSQL Dialect = "postgresql"
	// DialectDuckDB uses Darkwing's DuckDB parser for statement targets.
	DialectDuckDB Dialect = "duckdb"
	// DialectSQLite uses generic SQL parsing and SQLite-compatible sanitization.
	DialectSQLite Dialect = "sqlite"
	// DialectMySQL uses generic SQL parsing and MySQL-compatible sanitization.
	DialectMySQL Dialect = "mysql"
)

// Summary returns a parameter-free, bounded description of one statement. It
// is useful for ad hoc SQL whose source does not provide a stable query name.
// Application-owned queries should still prefer explicit names.
func Summary(dialect Dialect, query string) string {
	if name := SQLCName(query); name != "" {
		return name
	}
	if dialect != DialectDuckDB {
		if operation := Operation(query); operation != "" {
			return operation
		}
		return "SQL"
	}
	stmt, err := parser.ParseStatement(context.Background(), query)
	if err != nil {
		return "SQL"
	}
	if _, ok := stmt.(*ast.SelectStatement); ok {
		if sanitized, safe := Sanitize(query); safe {
			if summary, found := selectSummary(sanitized); found {
				return summary
			}
		}
	}
	return statementSummary(stmt)
}

func selectSummary(sanitized string) (string, bool) {
	keywords := topLevelSQLKeywords(sanitized)
	selectAt, foundSelect := keywords["SELECT"]
	fromAt, foundFrom := keywords["FROM"]
	if !foundSelect || foundFrom && fromAt <= selectAt {
		return "", false
	}
	clause := func(start int, endKeys ...string) string {
		end := len(sanitized)
		for _, key := range endKeys {
			if at, ok := keywords[key]; ok && at > start && at < end {
				end = at
			}
		}
		return strings.TrimSpace(sanitized[start:end])
	}
	parts := []string{shortenSQL(clause(selectAt, "FROM", "WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET"), 64)}
	if foundFrom {
		parts = append(parts, shortenSQL(clause(fromAt, "WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET"), 48))
	}
	for _, key := range []string{"WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET"} {
		if at, ok := keywords[key]; ok {
			parts = append(parts, shortenSQL(clause(at, followingSQLClauses(key)...), 64))
		}
	}
	return strings.Join(parts, " ") + " · " + strconv.Itoa(countPlaceholders(sanitized)) + " args", true
}

func followingSQLClauses(key string) []string {
	clauses := []string{"WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET"}
	for i, clause := range clauses {
		if clause == key {
			return clauses[i+1:]
		}
	}
	return nil
}

func topLevelSQLKeywords(s string) map[string]int {
	keywords := map[string]int{}
	for i, depth := 0, 0; i < len(s); {
		switch s[i] {
		case '\'', '"', '`':
			end, ok := skipQuoted(s, i, s[i])
			if !ok {
				return keywords
			}
			i = end
			continue
		case '(':
			depth++
			i++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if depth == 0 {
			for _, key := range []string{"GROUP BY", "ORDER BY", "SELECT", "FROM", "WHERE", "HAVING", "LIMIT", "OFFSET"} {
				if _, seen := keywords[key]; !seen && sqlKeywordAt(s, i, key) {
					keywords[key] = i
					i += len(key)
					goto next
				}
			}
		}
		i++
	next:
	}
	return keywords
}

func sqlKeywordAt(s string, start int, key string) bool {
	end := start + len(key)
	if end > len(s) || !strings.EqualFold(s[start:end], key) {
		return false
	}
	isWord := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	return (start == 0 || !isWord(s[start-1])) && (end == len(s) || !isWord(s[end]))
}

func shortenSQL(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max-1]) + "…"
}

func countPlaceholders(s string) int {
	return strings.Count(s, "?")
}

func statementSummary(stmt ast.Stmt) string {
	switch value := stmt.(type) {
	case *ast.SelectStatement:
		return withTarget("SELECT", selectTarget(value))
	case *ast.InsertStatement:
		return withTarget("INSERT", value.Table)
	case *ast.UpdateStatement:
		return withTarget("UPDATE", tableTarget(value.Table))
	case *ast.DeleteStatement:
		return withTarget("DELETE", tableTarget(value.Table))
	case *ast.MergeIntoStatement:
		return withTarget("MERGE", tableTarget(value.Target))
	case *ast.ExplainStatement:
		return "EXPLAIN " + statementSummary(value.Statement)
	default:
		return "SQL"
	}
}

func withTarget(operation, target string) string {
	if target == "" {
		return operation
	}
	return operation + " " + target
}

func selectTarget(stmt *ast.SelectStatement) string { return queryNodeTarget(stmt.Node) }

func queryNodeTarget(node ast.QueryNode) string {
	switch value := node.(type) {
	case *ast.SelectNode:
		return tableTarget(value.FromTable)
	case *ast.SetOperationNode:
		if len(value.Inputs) > 0 {
			return queryNodeTarget(value.Inputs[0])
		}
	}
	return ""
}

func tableTarget(table ast.TableRef) string {
	switch value := table.(type) {
	case *ast.BaseTableRef:
		return value.TableName
	case *ast.JoinRef:
		return tableTarget(value.Left)
	case *ast.SubqueryRef:
		return selectTarget(value.Subquery)
	}
	return ""
}
