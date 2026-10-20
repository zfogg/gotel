// Package sqltrace creates low-cardinality database span names and safe SQL
// telemetry attributes without recording bind values.
package sqltrace

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"unicode"

	"go.opentelemetry.io/otel/attribute"
)

const (
	// QueryTextKey contains a sanitized SQL template, never interpolated values.
	QueryTextKey = attribute.Key("db.query.text")
	// QuerySummaryKey contains the stable application-owned operation name.
	QuerySummaryKey = attribute.Key("db.query.summary")
	// ArgumentCountKey contains the number of bind arguments supplied at runtime.
	ArgumentCountKey = attribute.Key("db.query.parameter.count")
	// SystemKey identifies the database system, such as postgresql or duckdb.
	SystemKey = attribute.Key("db.system.name")
	// LegacySystemKey preserves db.system for collectors and dashboards built
	// before the semantic-convention key moved to db.system.name.
	LegacySystemKey = attribute.Key("db.system")
	// NamespaceKey identifies the logical database name.
	NamespaceKey = attribute.Key("db.namespace")
	// OperationKey contains the SQL operation, such as SELECT or INSERT.
	OperationKey = attribute.Key("db.operation.name")
)

type contextKey struct{}
type skipKey struct{}

// Namer validates and qualifies application-owned operation names.
type Namer struct {
	// Prefix is prepended to simple operation names, for example spaniel.storage.
	Prefix string
}

// Name returns <prefix>.<simple>. A simple name begins with an ASCII letter and
// otherwise contains only ASCII letters, digits, and underscores.
func (n Namer) Name(simple string) string {
	simple = strings.TrimSpace(simple)
	if !validSimpleName(simple) {
		panic(fmt.Sprintf("SQL query name must match [A-Za-z][A-Za-z0-9_]*: %q", simple))
	}
	prefix := strings.Trim(strings.TrimSpace(n.Prefix), ".")
	if prefix == "" {
		return simple
	}
	return prefix + "." + simple
}

// WithName carries one qualified, stable database operation name through ctx.
func (n Namer) WithName(ctx context.Context, simple string) context.Context {
	return context.WithValue(ctx, contextKey{}, n.Name(simple))
}

// NameFromContext returns the name installed by Namer.WithName.
func NameFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	name, _ := ctx.Value(contextKey{}).(string)
	return name
}

// WithoutTracing suppresses SQL spans for recursive telemetry storage or other
// deliberately untraced database work.
func WithoutTracing(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipKey{}, true)
}

// TracingDisabled reports whether WithoutTracing was applied.
func TracingDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	disabled, _ := ctx.Value(skipKey{}).(bool)
	return disabled
}

func validSimpleName(name string) bool {
	if name == "" || !isASCIILetter(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isASCIILetter(name[i]) && (name[i] < '0' || name[i] > '9') && name[i] != '_' {
			return false
		}
	}
	return true
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// CallerNamer returns the first application function name found above a
// database adapter. matchPrefix should be a fully qualified Go package or
// receiver prefix. The returned name is suitable for Namer.Name.
func CallerNamer(matchPrefix string) func() string {
	return func() string {
		pcs := make([]uintptr, 96)
		n := runtime.Callers(2, pcs)
		for _, pc := range pcs[:n] {
			fn := runtime.FuncForPC(pc)
			if fn == nil || !strings.HasPrefix(fn.Name(), matchPrefix) {
				continue
			}
			name := strings.TrimPrefix(fn.Name(), matchPrefix)
			name = strings.TrimPrefix(name, ".")
			if close := strings.Index(name, ")."); close >= 0 {
				name = name[close+2:]
			}
			if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
				name = name[slash+1:]
			}
			if dot := strings.IndexByte(name, '.'); dot >= 0 {
				name = name[:dot]
			}
			name = strings.TrimSuffix(name, "-fm")
			if validSimpleName(name) {
				return name
			}
		}
		return ""
	}
}

// Metadata is safe, bounded information about one SQL operation.
type Metadata struct {
	// Name is the qualified, low-cardinality span name.
	Name string
	// Operation is the upper-case SQL verb when one can be determined.
	Operation string
	// SanitizedSQL is the safe query template, or empty when sanitization fails.
	SanitizedSQL string
	// ArgumentCount is the number of bind values passed separately from SQL.
	ArgumentCount int
}

// Inspect extracts sqlc names, SQL operation, a sanitized template, and the
// actual bind argument count. explicitName wins when non-empty.
func Inspect(namer Namer, explicitName, statement string, args []any) Metadata {
	operation := Operation(statement)
	simple := strings.TrimSpace(explicitName)
	if simple == "" {
		simple = SQLCName(statement)
	}
	if simple == "" {
		simple = operation
	}
	if simple == "" {
		simple = "Unknown"
	}
	name := namer.Name(normalizeSimpleName(simple))
	sanitized, _ := Sanitize(statement)
	return Metadata{Name: name, Operation: strings.ToUpper(operation), SanitizedSQL: sanitized, ArgumentCount: len(args)}
}

// Attributes returns standard database attributes plus safe query metadata.
func (m Metadata) Attributes(system, namespace string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{QuerySummaryKey.String(m.Name), ArgumentCountKey.Int(m.ArgumentCount)}
	if system != "" {
		attrs = append(attrs, SystemKey.String(system), LegacySystemKey.String(system))
	}
	if namespace != "" {
		attrs = append(attrs, NamespaceKey.String(namespace))
	}
	if m.Operation != "" {
		attrs = append(attrs, OperationKey.String(m.Operation))
	}
	if m.SanitizedSQL != "" {
		attrs = append(attrs, QueryTextKey.String(m.SanitizedSQL))
	}
	return attrs
}

// SQLCName extracts a sqlc -- name: QueryName directive.
func SQLCName(statement string) string {
	for _, line := range strings.Split(statement, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-- name:") {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "-- name:")))
		if len(fields) > 0 && validSimpleName(fields[0]) {
			return fields[0]
		}
	}
	return ""
}

// Operation returns the first SQL keyword after leading comments.
func Operation(statement string) string {
	clean, _ := stripLeadingComments(statement)
	fields := strings.Fields(clean)
	if len(fields) == 0 {
		return ""
	}
	verb := strings.TrimFunc(fields[0], func(r rune) bool { return !unicode.IsLetter(r) })
	if verb == "" {
		return ""
	}
	return strings.ToUpper(verb)
}

func normalizeSimpleName(value string) string {
	if validSimpleName(value) {
		return value
	}
	var b strings.Builder
	upper := true
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if b.Len() == 0 && unicode.IsDigit(r) {
				b.WriteString("Query")
			}
			if upper {
				r = unicode.ToUpper(r)
			}
			b.WriteRune(r)
			upper = false
		} else {
			upper = true
		}
	}
	if out := b.String(); validSimpleName(out) {
		return out
	}
	return "Unknown"
}
