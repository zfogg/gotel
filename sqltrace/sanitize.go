package sqltrace

import (
	"strings"
	"unicode/utf8"
)

// DefaultMaxQueryTextBytes bounds db.query.text while retaining enough SQL
// shape for diagnostics. Applications may override it with SanitizeOptions.
const DefaultMaxQueryTextBytes = 4096

// SanitizeOptions controls dialect-specific SQL template sanitization.
type SanitizeOptions struct {
	// Dialect selects string escaping and placeholder rules.
	Dialect Dialect
	// MaxQueryTextBytes caps the emitted UTF-8 template. Zero uses
	// DefaultMaxQueryTextBytes; a negative value disables truncation.
	MaxQueryTextBytes int
	// CollapseInLists replaces lists containing only placeholders with IN (?).
	CollapseInLists bool
}

// SanitizedQuery is safe SQL template metadata.
type SanitizedQuery struct {
	// Text is SQL shape with comments removed and literals replaced by ?.
	Text string
	// Truncated reports whether Text was shortened to MaxQueryTextBytes.
	Truncated bool
}

// DefaultSanitizeOptions returns the recommended safe policy for dialect.
func DefaultSanitizeOptions(dialect Dialect) SanitizeOptions {
	return SanitizeOptions{Dialect: dialect, MaxQueryTextBytes: DefaultMaxQueryTextBytes, CollapseInLists: true}
}

// Sanitize applies the bounded generic-dialect policy. false means the input
// could not be tokenized safely; callers must omit db.query.text in that case.
func Sanitize(statement string) (string, bool) {
	result, ok := SanitizeWithOptions(statement, DefaultSanitizeOptions(DialectGeneric))
	return result.Text, ok
}

// SanitizeDialect applies the recommended safe policy for dialect.
func SanitizeDialect(dialect Dialect, statement string) (SanitizedQuery, bool) {
	return SanitizeWithOptions(statement, DefaultSanitizeOptions(dialect))
}

// SanitizeWithOptions removes comments and replaces literals and bind
// placeholders with ?. It recognizes PostgreSQL, DuckDB, SQLite, and MySQL
// quoting forms and fails closed on malformed or ambiguous quoted content.
func SanitizeWithOptions(statement string, options SanitizeOptions) (SanitizedQuery, bool) {
	if options.Dialect == "" {
		options.Dialect = DialectGeneric
	}
	if options.MaxQueryTextBytes == 0 {
		options.MaxQueryTextBytes = DefaultMaxQueryTextBytes
	}

	var out strings.Builder
	space := false
	for i := 0; i < len(statement); {
		if isSpace(statement[i]) {
			space = true
			i++
			continue
		}
		if i+1 < len(statement) && statement[i:i+2] == "--" {
			i += 2
			for i < len(statement) && statement[i] != '\n' {
				i++
			}
			space = true
			continue
		}
		if options.Dialect == DialectMySQL && statement[i] == '#' {
			for i < len(statement) && statement[i] != '\n' {
				i++
			}
			space = true
			continue
		}
		if i+1 < len(statement) && statement[i:i+2] == "/*" {
			end, ok := skipBlockComment(statement, i)
			if !ok {
				return SanitizedQuery{}, false
			}
			i = end
			space = true
			continue
		}
		if space && out.Len() > 0 {
			out.WriteByte(' ')
		}
		space = false

		if tag, contentStart, ok := dollarQuote(statement, i); ok {
			closeAt := strings.Index(statement[contentStart:], tag)
			if closeAt < 0 {
				return SanitizedQuery{}, false
			}
			out.WriteByte('?')
			i = contentStart + closeAt + len(tag)
			continue
		}
		if quoteAt, ok := stringLiteralStart(statement, i, options.Dialect); ok {
			end, safe := skipStringLiteral(statement, quoteAt, allowsBackslashEscapes(options.Dialect, statement[i:quoteAt]))
			if !safe {
				return SanitizedQuery{}, false
			}
			out.WriteByte('?')
			i = end
			continue
		}
		if options.Dialect == DialectMySQL && statement[i] == '"' {
			end, safe := skipDelimitedString(statement, i, '"', true)
			if !safe {
				return SanitizedQuery{}, false
			}
			out.WriteByte('?')
			i = end
			continue
		}
		if statement[i] == '"' || statement[i] == '`' || statement[i] == '[' {
			end, ok := skipIdentifier(statement, i)
			if !ok {
				return SanitizedQuery{}, false
			}
			out.WriteString(statement[i:end])
			i = end
			continue
		}
		if end, ok := placeholderEnd(statement, i); ok {
			out.WriteByte('?')
			i = end
			continue
		}
		if numericStart(statement, i) {
			out.WriteByte('?')
			i = numericEnd(statement, i)
			continue
		}
		if isIdentStart(statement[i]) {
			end := i + 1
			for end < len(statement) && isIdentPart(statement[end]) {
				end++
			}
			word := statement[i:end]
			if strings.EqualFold(word, "true") || strings.EqualFold(word, "false") || strings.EqualFold(word, "null") {
				out.WriteByte('?')
			} else {
				out.WriteString(word)
			}
			i = end
			continue
		}
		out.WriteByte(statement[i])
		i++
	}

	text := strings.TrimSpace(out.String())
	if options.CollapseInLists {
		text = collapsePlaceholderLists(text)
	}
	result := SanitizedQuery{Text: text}
	if options.MaxQueryTextBytes > 0 && len(result.Text) > options.MaxQueryTextBytes {
		result.Text = result.Text[:options.MaxQueryTextBytes]
		for !utf8.ValidString(result.Text) {
			result.Text = result.Text[:len(result.Text)-1]
		}
		result.Text = strings.TrimSpace(result.Text)
		result.Truncated = true
	}
	return result, true
}

func stripLeadingComments(statement string) (string, bool) {
	result, ok := SanitizeWithOptions(statement, SanitizeOptions{Dialect: DialectGeneric, MaxQueryTextBytes: -1})
	return result.Text, ok
}

func skipBlockComment(statement string, start int) (int, bool) {
	depth := 1
	for i := start + 2; i < len(statement); {
		switch {
		case i+1 < len(statement) && statement[i:i+2] == "/*":
			depth++
			i += 2
		case i+1 < len(statement) && statement[i:i+2] == "*/":
			depth--
			i += 2
			if depth == 0 {
				return i, true
			}
		default:
			i++
		}
	}
	return len(statement), false
}

func dollarQuote(s string, start int) (tag string, end int, ok bool) {
	if start >= len(s) || s[start] != '$' {
		return "", start, false
	}
	i := start + 1
	if i < len(s) && s[i] == '$' {
		return "$$", i + 1, true
	}
	if i >= len(s) || !isIdentStart(s[i]) {
		return "", start, false
	}
	for i < len(s) && (isIdentStart(s[i]) || isDigit(s[i])) {
		i++
	}
	if i < len(s) && s[i] == '$' {
		return s[start : i+1], i + 1, true
	}
	return "", start, false
}

func stringLiteralStart(s string, start int, dialect Dialect) (int, bool) {
	if s[start] == '\'' {
		return start, true
	}
	if start+1 < len(s) && strings.ContainsRune("EeBbXxNn", rune(s[start])) && s[start+1] == '\'' {
		return start + 1, true
	}
	if start+2 < len(s) && (s[start] == 'U' || s[start] == 'u') && s[start+1] == '&' && s[start+2] == '\'' {
		return start + 2, true
	}
	if dialect == DialectMySQL && s[start] == '_' {
		i := start + 1
		for i < len(s) && isIdentPart(s[i]) {
			i++
		}
		if i < len(s) && s[i] == '\'' {
			return i, true
		}
	}
	return start, false
}

func allowsBackslashEscapes(_ Dialect, _ string) bool {
	// Conservatively recognize backslash escaping for every dialect. PostgreSQL
	// may enable it for ordinary strings through standard_conforming_strings,
	// and treating it as data is safer than accidentally exposing a suffix.
	return true
}

func skipStringLiteral(s string, quoteAt int, backslash bool) (int, bool) {
	return skipDelimitedString(s, quoteAt, '\'', backslash)
}

func skipDelimitedString(s string, quoteAt int, quote byte, backslash bool) (int, bool) {
	for i := quoteAt + 1; i < len(s); i++ {
		if backslash && s[i] == '\\' {
			if i+1 >= len(s) {
				return len(s), false
			}
			i++
			continue
		}
		if s[i] != quote {
			continue
		}
		if i+1 < len(s) && s[i+1] == '\'' {
			i++
			continue
		}
		return i + 1, true
	}
	return len(s), false
}

func skipIdentifier(s string, start int) (int, bool) {
	close := s[start]
	if close == '[' {
		close = ']'
	}
	for i := start + 1; i < len(s); i++ {
		if s[i] != close {
			continue
		}
		if i+1 < len(s) && s[i+1] == close {
			i++
			continue
		}
		return i + 1, true
	}
	return len(s), false
}

// skipQuoted is retained for the summary parser, which may inspect already
// sanitized SQL containing quoted identifiers.
func skipQuoted(s string, start int, quote byte) (int, bool) {
	if quote == '\'' {
		return skipStringLiteral(s, start, true)
	}
	return skipIdentifier(s, start)
}

func placeholderEnd(s string, start int) (int, bool) {
	if s[start] == '?' {
		i := start + 1
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		return i, true
	}
	if s[start] == '$' && start+1 < len(s) && isDigit(s[start+1]) {
		i := start + 2
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		return i, true
	}
	if (s[start] == ':' || s[start] == '@') && start+1 < len(s) && isIdentStart(s[start+1]) {
		if s[start] == ':' && start > 0 && s[start-1] == ':' {
			return start, false
		}
		i := start + 2
		for i < len(s) && isIdentPart(s[i]) {
			i++
		}
		return i, true
	}
	if s[start] == '%' && start+1 < len(s) {
		if s[start+1] == 's' {
			return start + 2, true
		}
		if s[start+1] == '(' {
			end := strings.Index(s[start+2:], ")s")
			if end >= 0 {
				return start + 2 + end + 2, true
			}
		}
	}
	return start, false
}

func numericStart(s string, i int) bool {
	return isDigit(s[i]) || s[i] == '.' && i+1 < len(s) && isDigit(s[i+1])
}

func numericEnd(s string, start int) int {
	i := start
	if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X' || s[i+1] == 'b' || s[i+1] == 'B') {
		i += 2
		for i < len(s) && (isHex(s[i]) || s[i] == '_') {
			i++
		}
		return i
	}
	for i < len(s) && (isDigit(s[i]) || s[i] == '_' || s[i] == '.') {
		i++
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		for i < len(s) && (isDigit(s[i]) || s[i] == '_') {
			i++
		}
	}
	return i
}

func collapsePlaceholderLists(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if i+2 <= len(s) && strings.EqualFold(s[i:i+2], "in") && (i == 0 || !isIdentPart(s[i-1])) {
			j := i + 2
			for j < len(s) && isSpace(s[j]) {
				j++
			}
			if j < len(s) && s[j] == '(' {
				if end, count, ok := placeholderListEnd(s, j); ok && count > 1 {
					out.WriteString(s[i:j])
					out.WriteString("(?)")
					i = end
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func placeholderListEnd(s string, open int) (end, count int, ok bool) {
	i := open + 1
	for {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) || s[i] != '?' {
			return open, 0, false
		}
		count++
		i++
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i < len(s) && s[i] == ')' {
			return i + 1, count, true
		}
		if i >= len(s) || s[i] != ',' {
			return open, 0, false
		}
		i++
	}
}

func isIdentStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) || c == '$' }
func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isHex(c byte) bool        { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func isSpace(c byte) bool      { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
