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
)

// Summary describes operations and their physical tables, independently of
// application query names. CTE names and aliases are resolved away; joins,
// subqueries and set operations contribute distinct tables in SQL source order.
// Writes retain their target separately from read dependencies. Table-free
// SELECTs identify called functions. Literals, predicates, columns and arguments
// are excluded, and output is bounded to 256 UTF-8 bytes and five targets per
// operation. Malformed quoting returns SQL rather than exposing input text.
func Summary(dialect Dialect, query string) string {
	safe, ok := SanitizeWithOptions(query, SanitizeOptions{Dialect: dialect, MaxQueryTextBytes: -1})
	if !ok {
		return "SQL"
	}
	tokens := summaryTokens(safe.Text)
	s := summarize(tokens, nil, 0)
	return s.text()
}

// summaryToken retains quoted identifiers as opaque tokens, never keywords.
type summaryToken struct {
	text string
	word bool
}

func (t summaryToken) is(s string) bool { return t.word && strings.EqualFold(t.text, s) }

func summaryTokens(sql string) []summaryToken {
	var out []summaryToken
	for i := 0; i < len(sql); {
		c := sql[i]
		if c <= ' ' {
			i++
			continue
		}
		start := i
		if c == '"' || c == '`' || c == '[' {
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
		} else if isIdentStart(c) || c >= 128 {
			i++
			for i < len(sql) && (isIdentStart(sql[i]) || isDigit(sql[i]) || sql[i] == '$' || sql[i] >= 128) {
				i++
			}
			out = append(out, summaryToken{text: sql[start:i], word: true})
		} else {
			i++
			out = append(out, summaryToken{text: sql[start:i]})
		}
	}
	return out
}

// querySummary separates writes from reads so a data-modifying CTE cannot
// conceal its side effects behind the outer SELECT.
type querySummary struct {
	op        string
	targets   []string
	reads     []string
	writes    []string
	functions []string
}

func appendUnique(dst []string, values ...string) []string {
	for _, v := range values {
		if v == "" {
			continue
		}
		found := false
		for _, old := range dst {
			if old == v {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
}

func (s *querySummary) absorb(child querySummary) {
	if child.op != "SELECT" && child.op != "VALUES" && child.op != "" && child.op != "SQL" {
		s.writes = appendUnique(s.writes, child.op+summaryTargets(child.targets))
	}
	s.reads = appendUnique(s.reads, child.reads...)
	s.writes = appendUnique(s.writes, child.writes...)
	s.functions = appendUnique(s.functions, child.functions...)
}

func summaryTargets(names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) > 5 {
		return " " + strings.Join(names[:5], ", ") + ", …"
	}
	return " " + strings.Join(names, ", ")
}

func (s querySummary) text() string {
	if s.op == "" {
		return "SQL"
	}
	text := s.op
	if s.op == "SELECT" {
		if len(s.reads) > 0 {
			text += summaryTargets(s.reads)
		} else {
			text += summaryTargets(s.functions)
		}
	} else {
		text += summaryTargets(s.targets)
		if len(s.reads) > 0 {
			text += " SELECT" + summaryTargets(s.reads)
		}
	}
	for _, write := range s.writes {
		text += "; " + write
	}
	if len(text) > 256 {
		text = text[:253]
		for len(text) > 0 && !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		text += "…"
	}
	return text
}

func identifierKey(name string) string {
	if strings.HasPrefix(name, "\"") {
		return strings.ReplaceAll(strings.Trim(name, "\""), "\"\"", "\"")
	}
	if strings.HasPrefix(name, "`") {
		return strings.Trim(name, "`")
	}
	if strings.HasPrefix(name, "[") {
		return strings.Trim(name, "[]")
	}
	return strings.ToLower(name)
}

func identifier(tokens []summaryToken, i int) (string, int) {
	if i >= len(tokens) {
		return "", i
	}
	t := tokens[i]
	if !t.word && !strings.HasPrefix(t.text, "\"") && !strings.HasPrefix(t.text, "`") && !strings.HasPrefix(t.text, "[") {
		return "", i
	}
	name := t.text
	i++
	for i+1 < len(tokens) && tokens[i].text == "." {
		name += "." + tokens[i+1].text
		i += 2
	}
	return name, i
}

// closingParen returns the matching delimiter, or len(tokens) on incomplete SQL.
func closingParen(tokens []summaryToken, start int) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(tokens)
}

func summarize(tokens []summaryToken, inherited map[string]querySummary, depth int) querySummary {
	if depth > 64 || len(tokens) == 0 {
		return querySummary{}
	}
	ctes := make(map[string]querySummary, len(inherited))
	for name, value := range inherited {
		ctes[name] = value
	}
	var definitions []querySummary
	i := 0
	if tokens[i].is("WITH") {
		i++
		recursive := false
		if i < len(tokens) && tokens[i].is("RECURSIVE") {
			recursive = true
			i++
		}
		for i < len(tokens) {
			name, next := identifier(tokens, i)
			if name == "" {
				break
			}
			i = next
			if i < len(tokens) && tokens[i].text == "(" {
				i = closingParen(tokens, i) + 1
			}
			if i >= len(tokens) || !tokens[i].is("AS") {
				break
			}
			i++
			if i < len(tokens) && tokens[i].is("NOT") {
				i++
			}
			if i < len(tokens) && tokens[i].is("MATERIALIZED") {
				i++
			}
			if i >= len(tokens) || tokens[i].text != "(" {
				break
			}
			end := closingParen(tokens, i)
			key := identifierKey(name)
			if recursive {
				ctes[key] = querySummary{} // recursive self-reference is not a physical table
			}
			def := summarize(tokens[i+1:end], ctes, depth+1)
			ctes[key] = def
			definitions = append(definitions, def)
			i = end + 1
			if i >= len(tokens) || tokens[i].text != "," {
				break
			}
			i++
		}
	}
	if i >= len(tokens) {
		return querySummary{}
	}
	if tokens[i].is("EXPLAIN") {
		i++
		if i < len(tokens) && tokens[i].text == "(" {
			i = closingParen(tokens, i) + 1
		}
		for i < len(tokens) && (tokens[i].is("ANALYZE") || tokens[i].is("VERBOSE") || tokens[i].is("QUERY") || tokens[i].is("PLAN")) {
			i++
		}
		if i >= len(tokens) {
			return querySummary{op: "EXPLAIN"}
		}
		s := summarize(tokens[i:], ctes, depth+1)
		s.op = "EXPLAIN " + s.op
		s.targets = appendUnique(s.targets, s.reads...)
		s.reads = nil
		return s
	}
	s := querySummary{}
	for _, op := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "MERGE", "COPY", "CALL", "TRUNCATE", "CREATE", "ALTER", "DROP", "VACUUM", "ANALYZE", "PRAGMA", "SHOW", "SET", "BEGIN", "COMMIT", "ROLLBACK", "VALUES"} {
		if tokens[i].is(op) {
			s.op = op
			break
		}
	}
	if s.op == "" {
		return s
	}
	start := i
	from := false
	expectRelation := false
	writeTarget := false
	if s.op == "UPDATE" || s.op == "CALL" || s.op == "COPY" || s.op == "VACUUM" || s.op == "ANALYZE" || s.op == "TRUNCATE" {
		expectRelation = true
		writeTarget = true
	}
	for i = start + 1; i < len(tokens); {
		t := tokens[i]
		relationGroup := expectRelation && t.text == "("
		if t.text == ";" {
			break
		}
		if s.op == "COPY" && (t.is("FROM") || t.is("TO")) {
			break
		}
		// IS [NOT] DISTINCT FROM compares expressions; it introduces no relation.
		if t.is("FROM") && i > start && tokens[i-1].is("DISTINCT") {
			i++
			continue
		}
		if expectRelation {
			if t.is("ONLY") || t.is("LATERAL") || t.is("TABLE") || t.is("IF") || t.is("NOT") || t.is("EXISTS") {
				i++
				continue
			}
			expectRelation = false
			if t.text != "(" {
				name, next := identifier(tokens, i)
				if name != "" {
					isFunction := next < len(tokens) && tokens[next].text == "("
					if writeTarget {
						s.targets = appendUnique(s.targets, name)
					} else if cte, ok := ctes[identifierKey(name)]; ok {
						s.absorb(cte)
					} else if !isFunction {
						s.reads = appendUnique(s.reads, name)
					} else {
						s.functions = appendUnique(s.functions, name)
					}
					writeTarget = false
					i = next
					continue
				}
			}
			writeTarget = false
		}
		if t.text == "(" {
			end := closingParen(tokens, i)
			child := summarize(tokens[i+1:end], ctes, depth+1)
			if child.op == "" && relationGroup {
				// A parenthesized join tree is a relation, not an expression.
				group := []summaryToken{{text: "SELECT", word: true}, {text: "FROM", word: true}}
				group = append(group, tokens[i+1:end]...)
				child = summarize(group, ctes, depth+1)
			}
			if child.op != "" {
				s.absorb(child)
			} else {
				// Expressions can contain scalar subqueries at any nesting depth.
				s.scanExpressions(tokens[i+1:end], ctes, depth+1)
			}
			i = end + 1
			continue
		}
		if t.is("FROM") || t.is("JOIN") || (t.is("USING") && (s.op == "DELETE" || s.op == "MERGE")) {
			expectRelation, from = true, true
			if t.is("FROM") && s.op == "DELETE" && len(s.targets) == 0 {
				writeTarget = true
			}
		} else if t.is("INTO") && (s.op == "INSERT" || s.op == "MERGE") && len(s.targets) == 0 {
			expectRelation, writeTarget = true, true
		} else if (t.is("TABLE") || t.is("INDEX") || t.is("VIEW") || t.is("SCHEMA")) && (s.op == "CREATE" || s.op == "ALTER" || s.op == "DROP") && len(s.targets) == 0 {
			expectRelation, writeTarget = true, true
		} else if t.text == "," && from {
			expectRelation = true
		} else if t.is("WHERE") || t.is("GROUP") || t.is("ORDER") || t.is("HAVING") || t.is("LIMIT") || t.is("RETURNING") || t.is("SET") || t.is("ON") || t.is("UNION") || t.is("INTERSECT") || t.is("EXCEPT") || t.is("WINDOW") || t.is("QUALIFY") {
			from = false
		}
		if s.op == "SELECT" && !summarySyntax(t) {
			if name, next := identifier(tokens, i); name != "" && next < len(tokens) && tokens[next].text == "(" {
				s.functions = appendUnique(s.functions, name)
				i = next
				continue
			}
		}
		i++
	}
	for _, def := range definitions {
		// Even an unreferenced data-modifying CTE executes.
		if def.op != "SELECT" && def.op != "VALUES" {
			s.absorb(def)
		}
	}
	return s
}

func summarySyntax(t summaryToken) bool {
	for _, keyword := range []string{"IN", "EXISTS", "OVER", "FILTER", "AS", "ON", "VALUES", "SELECT", "DISTINCT", "CAST", "CASE", "ANY", "ALL"} {
		if t.is(keyword) {
			return true
		}
	}
	return false
}

func (s *querySummary) scanExpressions(tokens []summaryToken, ctes map[string]querySummary, depth int) {
	if depth > 64 {
		return
	}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "(" {
			continue
		}
		end := closingParen(tokens, i)
		child := summarize(tokens[i+1:end], ctes, depth+1)
		if child.op != "" {
			s.absorb(child)
		} else {
			s.scanExpressions(tokens[i+1:end], ctes, depth+1)
		}
		i = end
	}
}
