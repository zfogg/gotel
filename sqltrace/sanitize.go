package sqltrace

import "strings"

// Sanitize replaces string, numeric, byte/bit, dollar-quoted, and bind
// literals with ? and removes comments. It preserves identifiers and SQL shape.
// false means the input contained an unterminated literal or comment; callers
// must then omit db.query.text rather than risk leaking data.
func Sanitize(statement string) (string, bool) {
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
		if i+1 < len(statement) && statement[i:i+2] == "/*" {
			end := strings.Index(statement[i+2:], "*/")
			if end < 0 {
				return "", false
			}
			i += end + 4
			space = true
			continue
		}
		if space && out.Len() > 0 {
			out.WriteByte(' ')
		}
		space = false

		if tag, end, ok := dollarQuote(statement, i); ok {
			closeAt := strings.Index(statement[end:], tag)
			if closeAt < 0 {
				return "", false
			}
			out.WriteByte('?')
			i = end + closeAt + len(tag)
			continue
		}
		if statement[i] == '\'' || ((statement[i] == 'E' || statement[i] == 'e' || statement[i] == 'B' || statement[i] == 'b' || statement[i] == 'X' || statement[i] == 'x') && i+1 < len(statement) && statement[i+1] == '\'') {
			if statement[i] != '\'' {
				i++
			}
			end, ok := skipQuoted(statement, i, '\'')
			if !ok {
				return "", false
			}
			out.WriteByte('?')
			i = end
			continue
		}
		if statement[i] >= '0' && statement[i] <= '9' {
			i++
			for i < len(statement) && ((statement[i] >= '0' && statement[i] <= '9') || strings.ContainsRune("._eE+-", rune(statement[i]))) {
				i++
			}
			out.WriteByte('?')
			continue
		}
		if statement[i] == '$' && i+1 < len(statement) && statement[i+1] >= '0' && statement[i+1] <= '9' {
			i += 2
			for i < len(statement) && statement[i] >= '0' && statement[i] <= '9' {
				i++
			}
			out.WriteByte('?')
			continue
		}
		if statement[i] == '?' {
			out.WriteByte('?')
			i++
			continue
		}
		if statement[i] == '"' || statement[i] == '`' {
			end, ok := skipQuoted(statement, i, statement[i])
			if !ok {
				return "", false
			}
			out.WriteString(statement[i:end])
			i = end
			continue
		}
		out.WriteByte(statement[i])
		i++
	}
	return strings.TrimSpace(out.String()), true
}

func stripLeadingComments(statement string) (string, bool) {
	sanitized, ok := Sanitize(statement)
	return sanitized, ok
}

func dollarQuote(s string, start int) (tag string, end int, ok bool) {
	if start >= len(s) || s[start] != '$' {
		return "", start, false
	}
	i := start + 1
	for i < len(s) && (s[i] == '_' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if i < len(s) && s[i] == '$' {
		return s[start : i+1], i + 1, true
	}
	return "", start, false
}

func skipQuoted(s string, start int, quote byte) (int, bool) {
	for i := start + 1; i < len(s); i++ {
		if s[i] != quote {
			continue
		}
		if i+1 < len(s) && s[i+1] == quote {
			i++
			continue
		}
		return i + 1, true
	}
	return len(s), false
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
