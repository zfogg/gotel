package sqltrace

// DefaultMaxQueryTextBytes bounds db.query.text while retaining enough SQL
// shape for diagnostics. Applications may override it with SanitizeOptions.
const DefaultMaxQueryTextBytes = 32 * 1024

// SanitizeOptions controls dialect-specific SQL template sanitization.
type SanitizeOptions struct {
	// Dialect selects string escaping and placeholder rules.
	Dialect Dialect
	// MaxQueryTextBytes caps the emitted UTF-8 template. Zero uses
	// DefaultMaxQueryTextBytes; a negative value disables truncation.
	MaxQueryTextBytes int
	// CollapseInLists replaces lists containing only placeholders with IN (?).
	CollapseInLists bool
	// OmitQueryText disables db.query.text collection entirely. Naming,
	// operation detection, and bind counts remain available.
	OmitQueryText bool
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

// SanitizeWithOptions uses the shared C scanner to remove comments and replace
// literals and bind placeholders. Malformed or ambiguous SQL fails closed.
func SanitizeWithOptions(statement string, options SanitizeOptions) (SanitizedQuery, bool) {
	if options.OmitQueryText {
		return SanitizedQuery{}, true
	}
	return sanitizeNative(statement, options)
}
