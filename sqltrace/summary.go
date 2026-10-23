package sqltrace

import (
	"strings"
	"unicode/utf8"
)

// Dialect selects SQL quoting, placeholder, and sanitization rules.
type Dialect string

const (
	// DialectGeneric accepts common SQL syntax.
	DialectGeneric Dialect = "generic"
	// DialectPostgreSQL recognizes PostgreSQL quoting and placeholders.
	DialectPostgreSQL Dialect = "postgresql"
	// DialectDuckDB recognizes DuckDB quoting and placeholders.
	DialectDuckDB Dialect = "duckdb"
	// DialectSQLite recognizes SQLite quoting and placeholders.
	DialectSQLite Dialect = "sqlite"
	// DialectMySQL recognizes MySQL quoting and placeholders.
	DialectMySQL Dialect = "mysql"
	// DefaultMaxSummaryBytes bounds structural summaries independently of SQL text.
	// The larger structural budget accommodates deeply involved CTE queries.
	DefaultMaxSummaryBytes = 4096
)

// Summary returns a bounded SQL skeleton, independently of application names.
// It retains CTE definitions, nested SELECTs, join types (including LATERAL),
// set operators, grouping/ordering, row bounds, locking and write structure.
// Join predicates and USING keys retain identifiers/operators with sanitized
// values; other predicates, projections and non-join function arguments are omitted.
// Only standalone integer LIMIT/OFFSET/FETCH counts are retained; bind values,
// expressions and sqlc.arg/narg references become ?. Summary never interpolates
// argument values. Malformed quoting returns SQL.
func Summary(dialect Dialect, query string) string {
	safe, ok := sanitizeWithOptions(query, SanitizeOptions{Dialect: dialect, MaxQueryTextBytes: -1}, true)
	if !ok {
		return "SQL"
	}
	shape := summarize(summaryTokens(safe.Text, dialect), 0)
	if shape == "" {
		return "SQL"
	}
	if len(shape) > DefaultMaxSummaryBytes {
		shape = shape[:DefaultMaxSummaryBytes-3]
		for !utf8.ValidString(shape) && len(shape) > 0 {
			shape = shape[:len(shape)-1]
		}
		shape = strings.TrimSpace(shape) + "…"
	}
	return shape
}

// summaryToken keeps quoted identifiers opaque and numeric tokens distinct.
type summaryToken struct {
	text  string
	word  bool
	space bool // Whitespace before this token in the sanitized template.
}

// is recognizes keywords without interpreting quoted identifiers as syntax.
func (t summaryToken) is(s string) bool { return t.word && strings.EqualFold(t.text, s) }

// summaryTokens tokenizes sanitized input, preserving only structural spelling.
func summaryTokens(sql string, dialect Dialect) []summaryToken {
	var out []summaryToken
	for i := 0; i < len(sql); {
		if sql[i] <= ' ' {
			i++
			continue
		}
		start := i
		spaced := i > 0 && sql[i-1] <= ' '
		switch c := sql[i]; {
		case c == '"' || c == '`' || c == '[' && bracketIdentifierDialect(dialect):
			close := c
			if c == '[' {
				close = ']'
			}
			i++
			for i < len(sql) {
				if sql[i] == close {
					i++
					if i < len(sql) && sql[i] == close {
						i++
						continue
					}
					break
				}
				i++
			}
			out = append(out, summaryToken{text: sql[start:i]})
		case isDigit(c):
			i = numericEnd(sql, i)
			out = append(out, summaryToken{text: sql[start:i]})
		case isIdentStart(c) || c >= 128:
			i++
			for i < len(sql) && (isIdentStart(sql[i]) || isDigit(sql[i]) || sql[i] == '$' || sql[i] >= 128) {
				i++
			}
			out = append(out, summaryToken{text: sql[start:i], word: true})
		default:
			i++
			out = append(out, summaryToken{text: sql[start:i]})
		}
		out[len(out)-1].space = spaced
	}
	return out
}

// identifierToken distinguishes identifiers from punctuation and numeric input.
func identifierToken(t summaryToken) bool {
	return t.word || strings.HasPrefix(t.text, "\"") || strings.HasPrefix(t.text, "`") || strings.HasPrefix(t.text, "[")
}

// identifier consumes a potentially schema-qualified name without its alias.
func identifier(tokens []summaryToken, i int) (string, int) {
	if i >= len(tokens) || !identifierToken(tokens[i]) {
		return "", i
	}
	name := tokens[i].text
	i++
	for i+1 < len(tokens) && tokens[i].text == "." && identifierToken(tokens[i+1]) {
		name += "." + tokens[i+1].text
		i += 2
	}
	return name, i
}

// closingParen finds a balanced group's end, tolerating incomplete SQL.
func closingParen(tokens []summaryToken, start int) int {
	return closingGroup(tokens, start, "(", ")")
}

// closingGroup finds a balanced parenthesis or array/subscript boundary.
func closingGroup(tokens []summaryToken, start int, open, close string) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		if tokens[i].text == open {
			depth++
		}
		if tokens[i].text == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(tokens)
}

// queryStart reports whether a group contains a statement rather than an expression.
func queryStart(t summaryToken) bool {
	for _, op := range []string{"SELECT", "WITH", "INSERT", "UPDATE", "DELETE", "MERGE", "VALUES", "EXPLAIN"} {
		if t.is(op) {
			return true
		}
	}
	return false
}

// nestedQueries finds SELECTs hidden in expressions without emitting their values.
func nestedQueries(tokens []summaryToken, depth int) []string {
	if depth > 64 {
		return []string{"…"}
	}
	var out []string
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "(" {
			continue
		}
		end := closingParen(tokens, i)
		body := tokens[i+1 : end]
		if len(body) > 0 && queryStart(body[0]) {
			label := ""
			if i > 0 && (tokens[i-1].is("EXISTS") || tokens[i-1].is("IN")) {
				label = strings.ToUpper(tokens[i-1].text) + " "
				if i > 1 && tokens[i-2].is("NOT") {
					label = "NOT " + label
				}
			}
			out = append(out, label+"("+summarize(body, depth+1)+")")
		} else {
			out = append(out, nestedQueries(body, depth+1)...)
		}
		i = end
	}
	return out
}

// clauseBoundary identifies query clauses at the current nesting level.
func clauseBoundary(t summaryToken) bool {
	for _, kw := range []string{"FROM", "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET", "FETCH", "FOR", "RETURNING", "UNION", "INTERSECT", "EXCEPT", "WINDOW", "QUALIFY", "SET", "USING"} {
		if t.is(kw) {
			return true
		}
	}
	return t.text == ";"
}

// clauseEnd skips balanced expressions so nested clauses keep their own scope.
func clauseEnd(tokens []summaryToken, start int) int {
	cases := 0
	for i := start; i < len(tokens); i++ {
		if tokens[i].text == "(" {
			i = closingParen(tokens, i)
			continue
		}
		if tokens[i].is("CASE") {
			cases++
			continue
		}
		if tokens[i].is("END") && cases > 0 {
			cases--
			continue
		}
		if cases > 0 {
			continue
		}
		if tokens[i].is("FROM") && i > 0 && tokens[i-1].is("DISTINCT") {
			continue
		}
		if tokens[i].is("GROUP") && i > 0 && tokens[i-1].is("WITHIN") {
			continue
		}
		if tokens[i].is("ON") && i+1 < len(tokens) && tokens[i+1].is("CONFLICT") {
			return i
		}
		if clauseBoundary(tokens[i]) || tokens[i].text == ")" {
			return i
		}
	}
	return len(tokens)
}

// rowBound preserves only static integer counts, never a computed/bound value.
func rowBound(tokens []summaryToken) string {
	if len(tokens) == 1 && tokens[0].is("ALL") {
		return "ALL"
	}
	if len(tokens) == 2 && (tokens[0].text == "-" || tokens[0].text == "+") {
		if staticInteger(tokens[1].text) {
			return tokens[0].text + tokens[1].text
		}
	}
	if len(tokens) == 1 && staticInteger(tokens[0].text) {
		return tokens[0].text
	}
	return "?"
}

// staticInteger rejects decimals, casts, expressions and placeholder spelling.
func staticInteger(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// boundList handles MySQL/SQLite LIMIT offset,count without flattening subqueries.
func boundList(tokens []summaryToken) string {
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text == "(" {
			i = closingParen(tokens, i)
			continue
		}
		if tokens[i].text == "," {
			return rowBound(tokens[:i]) + ", " + rowBound(tokens[i+1:])
		}
	}
	return rowBound(tokens)
}

// joinAt recognizes standard and DuckDB join modifiers in their original order.
func joinAt(tokens []summaryToken, i int) (string, int) {
	start := i
	var parts []string
	for i < len(tokens) {
		t := tokens[i]
		if t.is("JOIN") {
			parts = append(parts, "JOIN")
			i++
			if i < len(tokens) && tokens[i].is("LATERAL") {
				parts = append(parts, "LATERAL")
				i++
			}
			return strings.Join(parts, " "), i
		}
		modifier := false
		for _, kw := range []string{"NATURAL", "LEFT", "RIGHT", "FULL", "OUTER", "INNER", "CROSS", "SEMI", "ANTI", "ASOF", "POSITIONAL"} {
			if t.is(kw) {
				parts = append(parts, kw)
				modifier = true
				break
			}
		}
		if !modifier {
			break
		}
		i++
	}
	return "", start
}

// relationSource renders tables, table functions and parenthesized query/join sources.
func relationSource(tokens []summaryToken, i, depth int) (string, int) {
	prefix := ""
	for i < len(tokens) && (tokens[i].is("ONLY") || tokens[i].is("LATERAL")) {
		prefix += strings.ToUpper(tokens[i].text) + " "
		i++
	}
	if i >= len(tokens) {
		return "…", i
	}
	if tokens[i].text == "(" {
		end := closingParen(tokens, i)
		body := tokens[i+1 : end]
		if len(body) > 0 && queryStart(body[0]) {
			return prefix + "(" + summarize(body, depth+1) + ")", end + 1
		}
		return prefix + "(" + relations(body, depth+1) + ")", end + 1
	}
	name, next := identifier(tokens, i)
	if name == "" {
		return "…", i + 1
	}
	if next < len(tokens) && tokens[next].text == "(" {
		end := closingParen(tokens, next)
		nested := nestedQueries(tokens[next+1:end], depth+1)
		if len(nested) > 0 {
			name += "(" + strings.Join(nested, ", ") + ")"
		} else {
			name += "(…)"
		}
		next = end + 1
	}
	return prefix + name, next
}

// relation retains aliases so qualified join keys can be resolved to sources.
func relation(tokens []summaryToken, i, depth int) (string, int) {
	name, next := relationSource(tokens, i, depth)
	if next+1 < len(tokens) && tokens[next].is("WITH") && tokens[next+1].is("ORDINALITY") {
		name += " WITH ORDINALITY"
		next += 2
	}
	if next >= len(tokens) {
		return name, next
	}
	explicit := tokens[next].is("AS")
	alias := next
	if explicit {
		alias++
	}
	if alias < len(tokens) && identifierToken(tokens[alias]) && !clauseBoundary(tokens[alias]) &&
		!tokens[alias].is("ON") && !tokens[alias].is("WHEN") && !tokens[alias].is("WITH") &&
		!tokens[alias].is("TABLESAMPLE") && !tokens[alias].is("PIVOT") && !tokens[alias].is("UNPIVOT") {
		if join, _ := joinAt(tokens, alias); join == "" {
			name += " AS " + tokens[alias].text
			next = alias + 1
			if next < len(tokens) && tokens[next].text == "(" {
				end := closingParen(tokens, next)
				name += joinExpression(tokens[next:end+min(1, len(tokens)-end)], depth+1)
				next = end + 1
			}
		}
	}
	return name, next
}

// joinExpression preserves sanitized predicate shape and nested query scopes.
// Summary's private numeric-preservation mode is only safe for row bounds:
// replace every numeric token here, including decimals and exponent notation.
func joinExpression(tokens []summaryToken, depth int) string {
	if depth > 64 {
		return "…"
	}
	var out strings.Builder
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if out.Len() > 0 && t.space {
			out.WriteByte(' ')
		}
		if t.text == "(" {
			end := closingParen(tokens, i)
			out.WriteByte('(')
			body := tokens[i+1 : end]
			if len(body) > 0 && queryStart(body[0]) {
				out.WriteString(summarize(body, depth+1))
			} else {
				out.WriteString(joinExpression(body, depth+1))
			}
			out.WriteByte(')')
			i = end
		} else if len(t.text) > 0 && (isDigit(t.text[0]) || t.text == "." && i+1 < len(tokens) && isDigit(tokens[i+1].text[0])) {
			out.WriteByte('?')
			if t.text == "." {
				i++
			}
		} else {
			out.WriteString(t.text)
		}
	}
	return out.String()
}

// joinConditionEnd keeps CASE/grouped expressions scoped and stops before the
// next joined/comma-separated source, never treating a nested join as a sibling.
func joinConditionEnd(tokens []summaryToken, start int) int {
	cases := 0
	for i := start; i < len(tokens); i++ {
		if tokens[i].text == "[" {
			i = closingGroup(tokens, i, "[", "]")
			continue
		}
		if tokens[i].text == "(" {
			i = closingParen(tokens, i)
			continue
		}
		if tokens[i].is("CASE") {
			cases++
		} else if tokens[i].is("END") && cases > 0 {
			cases--
		}
		if cases == 0 {
			if join, _ := joinAt(tokens, i); join != "" || tokens[i].text == "," {
				return i
			}
		}
	}
	return len(tokens)
}

// relations retains sources, aliases, join topology and sanitized conditions.
func relations(tokens []summaryToken, depth int) string {
	if depth > 64 {
		return "…"
	}
	if len(tokens) == 0 {
		return "…"
	}
	first, i := relation(tokens, 0, depth)
	parts := []string{first}
	for i < len(tokens) {
		if tokens[i].is("ON") || tokens[i].is("USING") {
			end := joinConditionEnd(tokens, i+1)
			parts = append(parts, strings.ToUpper(tokens[i].text), joinExpression(tokens[i+1:end], depth+1))
			i = end
			continue
		}
		if join, next := joinAt(tokens, i); join != "" {
			source, end := relation(tokens, next, depth)
			parts = append(parts, join, source)
			i = end
			continue
		}
		if tokens[i].text == "," {
			source, end := relation(tokens, i+1, depth)
			parts[len(parts)-1] += ","
			parts = append(parts, source)
			i = end
			continue
		}
		if tokens[i].text == "(" {
			end := closingParen(tokens, i)
			parts = append(parts, nestedQueries(tokens[i:end+min(1, len(tokens)-end)], depth+1)...)
			i = end + 1
			continue
		}
		i++
	}
	return strings.Join(parts, " ")
}

// sourceEnd distinguishes MERGE/DELETE USING sources from JOIN USING keys.
func sourceEnd(tokens []summaryToken, start int) int {
	cases := 0
	joined := false
	for i := start; i < len(tokens); i++ {
		if tokens[i].text == "(" {
			i = closingParen(tokens, i)
			continue
		}
		if tokens[i].is("CASE") {
			cases++
			continue
		}
		if tokens[i].is("END") && cases > 0 {
			cases--
			continue
		}
		if cases > 0 {
			continue
		}
		if tokens[i].is("FROM") && i > 0 && tokens[i-1].is("DISTINCT") {
			continue
		}
		if tokens[i].is("WHEN") {
			return i
		}
		if join, _ := joinAt(tokens, i); join != "" {
			joined = true
		}
		if tokens[i].is("ON") && i+1 < len(tokens) && tokens[i+1].is("CONFLICT") {
			return i
		}
		if tokens[i].is("USING") && joined {
			continue
		}
		if clauseBoundary(tokens[i]) || tokens[i].is("ON") {
			// ON belongs to a join's condition inside a FROM source. Keep it
			// there; relations renders its sanitized predicate and later joins.
			if tokens[i].is("ON") {
				continue
			}
			return i
		}
	}
	return len(tokens)
}

// expressionClause renders clause presence and all nested query scopes.
func expressionClause(label string, tokens []summaryToken, depth int) string {
	nested := nestedQueries(tokens, depth+1)
	if len(nested) == 0 {
		return label + " …"
	}
	return label + " … " + strings.Join(nested, " ")
}

// summarize renders a statement without expanding CTE references repeatedly.
// This retains nested bounds and join scopes even in very large CTE graphs.
func summarize(tokens []summaryToken, depth int) string {
	if depth > 64 {
		return "…"
	}
	if len(tokens) == 0 {
		return ""
	}
	i := 0
	var prefix string
	if tokens[0].is("WITH") {
		i++
		keyword := "WITH"
		if i < len(tokens) && tokens[i].is("RECURSIVE") {
			keyword += " RECURSIVE"
			i++
		}
		var definitions []string
		for i < len(tokens) {
			name, next := identifier(tokens, i)
			if name == "" {
				return "SQL"
			}
			i = next
			if i < len(tokens) && tokens[i].text == "(" {
				i = closingParen(tokens, i) + 1
			}
			if i >= len(tokens) || !tokens[i].is("AS") {
				return "SQL"
			}
			i++
			materialized := ""
			if i < len(tokens) && tokens[i].is("NOT") {
				materialized = " NOT"
				i++
			}
			if i < len(tokens) && tokens[i].is("MATERIALIZED") {
				materialized += " MATERIALIZED"
				i++
			}
			if i >= len(tokens) || tokens[i].text != "(" {
				return "SQL"
			}
			end := closingParen(tokens, i)
			definitions = append(definitions, name+" AS"+materialized+" ("+summarize(tokens[i+1:end], depth+1)+")")
			i = end + 1
			if i >= len(tokens) || tokens[i].text != "," {
				break
			}
			i++
		}
		prefix = keyword + " " + strings.Join(definitions, ", ") + " "
	}
	if i >= len(tokens) {
		return strings.TrimSpace(prefix)
	}
	if tokens[i].is("EXPLAIN") {
		i++
		options := ""
		if i < len(tokens) && tokens[i].text == "(" {
			end := closingParen(tokens, i)
			for _, t := range tokens[i+1 : end] {
				if t.is("ANALYZE") {
					options = " ANALYZE"
				}
			}
			i = end + 1
		}
		for i < len(tokens) && (tokens[i].is("ANALYZE") || tokens[i].is("VERBOSE") || tokens[i].is("QUERY") || tokens[i].is("PLAN")) {
			if tokens[i].is("ANALYZE") {
				options = " ANALYZE"
			}
			i++
		}
		if i >= len(tokens) {
			return "EXPLAIN" + options
		}
		return "EXPLAIN" + options + " " + summarize(tokens[i:], depth+1)
	}
	if tokens[i].text == "(" {
		end := closingParen(tokens, i)
		return prefix + summaryTail(tokens, end+1, "SELECT", []string{"(" + summarize(tokens[i+1:end], depth+1) + ")"}, depth)
	}
	op := ""
	for _, candidate := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "MERGE", "COPY", "CALL", "TRUNCATE", "CREATE", "ALTER", "DROP", "VACUUM", "ANALYZE", "PRAGMA", "SHOW", "SET", "BEGIN", "COMMIT", "ROLLBACK", "VALUES"} {
		if tokens[i].is(candidate) {
			op = candidate
			break
		}
	}
	if op == "" {
		return "SQL"
	}
	i++
	parts := []string{op}
	if op == "CREATE" {
		j := i
		unique := j < len(tokens) && tokens[j].is("UNIQUE")
		if unique {
			j++
		}
		if j < len(tokens) && tokens[j].is("INDEX") {
			j++
			if j < len(tokens) && tokens[j].is("CONCURRENTLY") {
				j++
			}
			if j+2 < len(tokens) && tokens[j].is("IF") && tokens[j+1].is("NOT") && tokens[j+2].is("EXISTS") {
				j += 3
			}
			name, next := identifier(tokens, j)
			if name != "" && next < len(tokens) && tokens[next].is("ON") {
				kind := "CREATE INDEX "
				if unique {
					kind = "CREATE UNIQUE INDEX "
				}
				return prefix + kind + name + " ON ..."
			}
		}
	}
	if op == "SELECT" {
		if i < len(tokens) && (tokens[i].is("DISTINCT") || tokens[i].is("ALL")) {
			if tokens[i].is("DISTINCT") {
				parts = append(parts, "DISTINCT")
			} else {
				parts = append(parts, "ALL")
			}
			i++
			if i < len(tokens) && tokens[i].is("ON") {
				parts = append(parts, "ON (…)")
				i++
				if i < len(tokens) && tokens[i].text == "(" {
					i = closingParen(tokens, i) + 1
				}
			}
		}
		end := clauseEnd(tokens, i)
		var functions []string
		for j := i; j < end; j++ {
			if tokens[j].text == "(" {
				j = closingParen(tokens, j)
				continue
			}
			name, next := identifier(tokens, j)
			if name != "" && next < end && tokens[next].text == "(" && !tokens[j].is("EXISTS") && !tokens[j].is("CAST") {
				close := closingParen(tokens, next)
				if close == next+1 {
					functions = append(functions, name+"()")
				} else {
					functions = append(functions, name+"(…)")
				}
				j = next - 1
			}
		}
		nested := nestedQueries(tokens[i:end], depth+1)
		if end < len(tokens) && tokens[end].is("FROM") {
			parts = append(parts, "…")
		} else if len(functions) > 0 {
			parts = append(parts, strings.Join(functions, ", "))
		} else {
			parts = append(parts, "…")
		}
		parts = append(parts, nested...)
		for j := i; j < end; j++ {
			if tokens[j].text == "(" {
				j = closingParen(tokens, j)
				continue
			}
			if tokens[j].is("OVER") {
				parts = append(parts, "OVER (…)")
			}
			if tokens[j].is("FILTER") {
				parts = append(parts, "FILTER (…)")
			}
			if tokens[j].is("WITHIN") && j+1 < end && tokens[j+1].is("GROUP") {
				parts = append(parts, "WITHIN GROUP (…)")
			}
		}
		i = end
	} else if op == "UPDATE" {
		end := clauseEnd(tokens, i)
		parts = append(parts, relations(tokens[i:end], depth+1))
		i = end
	} else if op == "CALL" || op == "COPY" || op == "VACUUM" || op == "ANALYZE" || op == "TRUNCATE" {
		if i < len(tokens) && tokens[i].is("TABLE") {
			i++
		}
		name, next := identifier(tokens, i)
		if name != "" {
			parts = append(parts, name)
			i = next
		}
		if op == "CALL" {
			return prefix + strings.Join(parts, " ")
		}
	} else if op == "VALUES" {
		return prefix + "VALUES …"
	}
	return prefix + summaryTail(tokens, i, op, parts, depth)
}

// summaryTail renders clauses and set branches following a statement's head.
func summaryTail(tokens []summaryToken, i int, op string, parts []string, depth int) string {
	for i < len(tokens) {
		t := tokens[i]
		if t.text == ";" || t.text == ")" {
			break
		}
		if t.is("UNION") || t.is("INTERSECT") || t.is("EXCEPT") {
			set := strings.ToUpper(t.text)
			i++
			if i < len(tokens) && (tokens[i].is("ALL") || tokens[i].is("DISTINCT")) {
				set += " " + strings.ToUpper(tokens[i].text)
				i++
			}
			if i < len(tokens) && tokens[i].is("BY") {
				set += " BY"
				i++
				if i < len(tokens) && tokens[i].is("NAME") {
					set += " NAME"
					i++
				}
			}
			rest := summarize(tokens[i:], depth+1)
			parts = append(parts, set, rest)
			break
		}
		if t.is("FROM") || t.is("USING") {
			label := strings.ToUpper(t.text)
			if op == "COPY" {
				break
			}
			end := sourceEnd(tokens, i+1)
			parts = append(parts, label, relations(tokens[i+1:end], depth+1))
			i = end
			continue
		}
		if t.is("INTO") && (op == "INSERT" || op == "MERGE") {
			name, next := identifier(tokens, i+1)
			if op == "MERGE" {
				name, next = relation(tokens, i+1, depth+1)
			}
			parts = append(parts, "INTO", name)
			i = next
			if i < len(tokens) && tokens[i].text == "(" {
				i = closingParen(tokens, i) + 1
			}
			continue
		}
		if t.is("SELECT") && op == "INSERT" {
			parts = append(parts, summarize(tokens[i:], depth+1))
			break
		}
		if t.is("LIMIT") || t.is("OFFSET") {
			end := clauseEnd(tokens, i+1)
			boundEnd := end
			if boundEnd > i+1 && (tokens[boundEnd-1].is("ROWS") || tokens[boundEnd-1].is("ROW")) {
				boundEnd--
			}
			parts = append(parts, strings.ToUpper(t.text), boundList(tokens[i+1:boundEnd]))
			parts = append(parts, nestedQueries(tokens[i+1:boundEnd], depth+1)...)
			i = end
			continue
		}
		if t.is("FETCH") {
			end := clauseEnd(tokens, i+1)
			j := i + 1
			kind := "FIRST"
			if j < end && (tokens[j].is("FIRST") || tokens[j].is("NEXT")) {
				kind = strings.ToUpper(tokens[j].text)
				j++
			}
			row := j
			for row < end && !tokens[row].is("ROW") && !tokens[row].is("ROWS") {
				row++
			}
			value := "1"
			if row > j {
				value = rowBound(tokens[j:row])
			}
			suffix := "ROWS ONLY"
			if row+2 < end && tokens[row+1].is("WITH") && tokens[row+2].is("TIES") {
				suffix = "ROWS WITH TIES"
			}
			parts = append(parts, "FETCH", kind, value, suffix)
			i = end
			continue
		}
		if t.is("ON") && i+1 < len(tokens) && tokens[i+1].is("CONFLICT") {
			i += 2
			if i < len(tokens) && tokens[i].text == "(" {
				i = closingParen(tokens, i) + 1
			}
			for i < len(tokens) && !tokens[i].is("DO") && !tokens[i].is("RETURNING") {
				i++
			}
			if i+1 < len(tokens) && tokens[i].is("DO") {
				parts = append(parts, "ON CONFLICT DO", strings.ToUpper(tokens[i+1].text))
				i += 2
				continue
			}
		}
		if t.is("FOR") {
			i++
			var lock []string
			for i < len(tokens) {
				matched := false
				for _, kw := range []string{"UPDATE", "NO", "KEY", "SHARE", "SKIP", "LOCKED", "NOWAIT"} {
					if tokens[i].is(kw) {
						lock = append(lock, kw)
						matched = true
						break
					}
				}
				if !matched && clauseBoundary(tokens[i]) {
					break
				}
				i++
			}
			parts = append(parts, "FOR", strings.Join(lock, " "))
			continue
		}
		if t.is("WHEN") && op == "MERGE" {
			i++
			branch := "WHEN"
			for i < len(tokens) && !tokens[i].is("THEN") {
				for _, kw := range []string{"MATCHED", "NOT", "BY", "SOURCE", "TARGET"} {
					if tokens[i].is(kw) {
						branch += " " + kw
						break
					}
				}
				if tokens[i].text == "(" {
					i = closingParen(tokens, i)
				}
				i++
			}
			if i+1 < len(tokens) && tokens[i].is("THEN") {
				for _, kw := range []string{"INSERT", "UPDATE", "DELETE", "DO"} {
					if tokens[i+1].is(kw) {
						branch += " THEN " + kw
						break
					}
				}
				i += 2
			}
			parts = append(parts, branch)
			continue
		}
		if t.is("WHERE") || t.is("HAVING") || t.is("QUALIFY") || t.is("SET") || t.is("RETURNING") || t.is("GROUP") || t.is("ORDER") || t.is("WINDOW") {
			label := strings.ToUpper(t.text)
			start := i + 1
			if (t.is("GROUP") || t.is("ORDER")) && start < len(tokens) && tokens[start].is("BY") {
				label += " BY"
				start++
			}
			end := clauseEnd(tokens, start)
			parts = append(parts, expressionClause(label, tokens[start:end], depth))
			i = end
			continue
		}
		if t.is("VALUES") {
			parts = append(parts, "VALUES …")
			i++
			continue
		}
		if t.text == "(" {
			end := closingParen(tokens, i)
			parts = append(parts, nestedQueries(tokens[i:end+min(1, len(tokens)-end)], depth+1)...)
			i = end + 1
			continue
		}
		i++
	}
	return strings.Join(parts, " ")
}
