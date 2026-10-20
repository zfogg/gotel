// Package sqltrace creates low-cardinality database span names and safe SQL
// telemetry attributes without recording bind values.
package sqltrace

import (
	"container/list"
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/zfogg/gotel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	// QueryTextKey contains a sanitized SQL template, never interpolated values.
	QueryTextKey = attribute.Key("db.query.text")
	// QuerySummaryKey contains a bounded operation-and-table SQL summary.
	QuerySummaryKey = attribute.Key("db.query.summary")
	// BindCountKey contains the number of bind arguments without colliding with
	// db.query.parameter.<key>, which is reserved for actual parameter values.
	BindCountKey = attribute.Key("gotel.db.query.bind_count")
	// QueryTextTruncatedKey reports that db.query.text was capped by policy.
	QueryTextTruncatedKey = attribute.Key("gotel.db.query.truncated")
	// SystemKey identifies the database system, such as postgresql or duckdb.
	SystemKey = attribute.Key("db.system.name")
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

// NewNamer validates prefix and returns a Namer. Prefix segments use the same
// identifier grammar as query names.
func NewNamer(prefix string) (Namer, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), ".")
	if prefix == "" {
		return Namer{}, nil
	}
	for _, segment := range strings.Split(prefix, ".") {
		if !validSimpleName(segment) {
			return Namer{}, fmt.Errorf("SQL query prefix segment must match [A-Za-z][A-Za-z0-9_]*: %q", segment)
		}
	}
	return Namer{Prefix: prefix}, nil
}

// MustNamer is NewNamer for static application configuration. It panics when
// prefix is invalid.
func MustNamer(prefix string) Namer {
	namer, err := NewNamer(prefix)
	if err != nil {
		panic(err)
	}
	return namer
}

// Parse validates a simple or already-qualified name and returns its canonical
// name under this Namer's prefix.
func (n Namer) Parse(name string) (string, error) {
	name = strings.TrimSpace(name)
	prefix := strings.Trim(strings.TrimSpace(n.Prefix), ".")
	if prefix != "" && strings.HasPrefix(name, prefix+".") {
		name = strings.TrimPrefix(name, prefix+".")
	}
	if !validSimpleName(name) {
		return "", fmt.Errorf("SQL query name must match [A-Za-z][A-Za-z0-9_]*: %q", name)
	}
	if prefix == "" {
		return name, nil
	}
	return prefix + "." + name, nil
}

// MustName is Parse for static query names. It panics when name is invalid.
func (n Namer) MustName(name string) string {
	qualified, err := n.Parse(name)
	if err != nil {
		panic(err)
	}
	return qualified
}

// Name returns <prefix>.<simple>. A simple name begins with an ASCII letter and
// otherwise contains only ASCII letters, digits, and underscores.
func (n Namer) Name(simple string) string {
	return n.MustName(simple)
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

var callerNames sync.Map

const defaultInspectionCacheSize = 1024

type inspectionCacheKey struct {
	statement string
	dialect   Dialect
	maxBytes  int
	collapse  bool
	omit      bool
}

type inspectionCacheValue struct {
	operation string
	summary   string
	simple    string
	sanitized SanitizedQuery
}

type inspectionCacheEntry struct {
	key   inspectionCacheKey
	value inspectionCacheValue
}

type inspectionCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[inspectionCacheKey]*list.Element
	order    *list.List
}

var inspections = newInspectionCache(defaultInspectionCacheSize)

func newInspectionCache(capacity int) *inspectionCache {
	return &inspectionCache{capacity: capacity, entries: make(map[inspectionCacheKey]*list.Element), order: list.New()}
}

func (c *inspectionCache) get(key inspectionCacheKey) (inspectionCacheValue, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return inspectionCacheValue{}, false
	}
	c.order.MoveToFront(element)
	return element.Value.(inspectionCacheEntry).value, true
}

func (c *inspectionCache) put(key inspectionCacheKey, value inspectionCacheValue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		element.Value = inspectionCacheEntry{key: key, value: value}
		c.order.MoveToFront(element)
		return
	}
	element := c.order.PushFront(inspectionCacheEntry{key: key, value: value})
	c.entries[key] = element
	if c.order.Len() <= c.capacity {
		return
	}
	oldest := c.order.Back()
	delete(c.entries, oldest.Value.(inspectionCacheEntry).key)
	c.order.Remove(oldest)
}

// CallerConfig controls source-owned fallback query names.
type CallerConfig struct {
	// MatchPrefix is a fully qualified Go package or receiver prefix.
	MatchPrefix string
	// Skip excludes adapter and forwarding methods such as Query and Exec.
	Skip []string
	// MaxDepth bounds stack inspection. Values below one use 96 frames.
	MaxDepth int
	// Fallback is returned when no stable matching function is found.
	Fallback string
}

// CallerNamer returns a cached caller resolver. matchPrefix may identify a Go
// package or a more specific receiver prefix. It skips compiler-generated
// wrappers and strips receiver and generic type syntax. An empty string is
// returned when no matching stable function exists; callers must use a
// deterministic fallback.
func CallerNamer(matchPrefix string) func() string {
	return NewCallerNamer(CallerConfig{MatchPrefix: matchPrefix})
}

// NewCallerNamer returns a cached caller resolver with configurable wrapper
// skips and deterministic fallback behavior.
func NewCallerNamer(config CallerConfig) func() string {
	skipped := make(map[string]struct{}, len(config.Skip))
	for _, name := range config.Skip {
		skipped[name] = struct{}{}
	}
	depth := config.MaxDepth
	if depth < 1 {
		depth = 96
	}
	return func() string {
		pcs := make([]uintptr, depth)
		n := runtime.Callers(2, pcs)
		for _, pc := range pcs[:n] {
			fn := runtime.FuncForPC(pc)
			if fn == nil || !strings.HasPrefix(fn.Name(), config.MatchPrefix) {
				continue
			}
			if cached, ok := callerNames.Load(pc); ok {
				candidate := cached.(string)
				if _, skip := skipped[candidate]; !skip {
					return candidate
				}
				continue
			}
			name := strings.TrimPrefix(fn.Name(), config.MatchPrefix)
			name = strings.TrimPrefix(name, ".")
			name = stripGenericSyntax(name)
			parts := strings.Split(name, ".")
			for i := len(parts) - 1; i >= 0; i-- {
				candidate := strings.TrimSuffix(parts[i], "-fm")
				if isCompilerFrame(candidate) {
					continue
				}
				if validSimpleName(candidate) {
					callerNames.Store(pc, candidate)
					if _, skip := skipped[candidate]; !skip {
						return candidate
					}
					break
				}
			}
		}
		return config.Fallback
	}
}

func stripGenericSyntax(name string) string {
	var out strings.Builder
	depth := 0
	for _, r := range name {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

func isCompilerFrame(name string) bool {
	for _, prefix := range []string{"func", "gowrap", "deferwrap"} {
		if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
			continue
		}
		allDigits := true
		for _, r := range name[len(prefix):] {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return true
		}
	}
	return false
}

// Metadata is safe, bounded information about one SQL operation.
type Metadata struct {
	// Name is the qualified, low-cardinality span name.
	Name string
	// Summary describes SQL operations and physical tables, not the span name.
	Summary string
	// Operation is the upper-case SQL verb when one can be determined.
	Operation string
	// SanitizedSQL is the safe query template, or empty when sanitization fails.
	SanitizedSQL string
	// QueryTextTruncated reports that SanitizedSQL was shortened by policy.
	QueryTextTruncated bool
	// ArgumentCount is the number of bind values passed separately from SQL.
	ArgumentCount int
}

// Inspect extracts sqlc names, SQL operation, a sanitized template, and the
// actual bind argument count. explicitName wins when non-empty.
func Inspect(namer Namer, explicitName, statement string, args []any) Metadata {
	return InspectDialect(namer, explicitName, statement, args, DialectGeneric)
}

// InspectDialect extracts safe metadata using dialect's recommended policy.
func InspectDialect(namer Namer, explicitName, statement string, args []any, dialect Dialect) Metadata {
	return InspectWithOptions(namer, explicitName, statement, args, DefaultSanitizeOptions(dialect))
}

// InspectWithOptions extracts safe metadata using an application policy.
func InspectWithOptions(namer Namer, explicitName, statement string, args []any, options SanitizeOptions) Metadata {
	if options.Dialect == "" {
		options.Dialect = DialectGeneric
	}
	if options.MaxQueryTextBytes == 0 {
		options.MaxQueryTextBytes = DefaultMaxQueryTextBytes
	}
	key := inspectionCacheKey{statement: statement, dialect: options.Dialect, maxBytes: options.MaxQueryTextBytes, collapse: options.CollapseInLists, omit: options.OmitQueryText}
	cached, ok := inspections.get(key)
	if !ok {
		cached.summary = Summary(options.Dialect, statement)
		cached.operation = strings.Fields(cached.summary)[0]
		if cached.operation == "SQL" {
			// Preserve Unknown naming for empty input and the historical verb
			// fallback for statements outside the structural summary grammar.
			cached.operation = Operation(statement)
		}
		cached.simple = SQLCName(statement)
		sanitized, safe := SanitizeWithOptions(statement, options)
		if safe {
			cached.sanitized = sanitized
		}
		inspections.put(key, cached)
	}
	operation := cached.operation
	simple := strings.TrimSpace(explicitName)
	qualified := ""
	if simple != "" {
		qualified, _ = namer.Parse(simple)
	}
	if simple == "" {
		simple = cached.simple
	}
	if simple == "" {
		simple = operation
	}
	if simple == "" {
		simple = "Unknown"
	}
	if qualified == "" {
		qualified = namer.Name(normalizeSimpleName(simple))
	}
	return Metadata{Name: qualified, Summary: cached.summary, Operation: strings.ToUpper(operation), SanitizedSQL: cached.sanitized.Text, QueryTextTruncated: cached.sanitized.Truncated, ArgumentCount: len(args)}
}

// Attributes returns standard database attributes plus safe query metadata.
func (m Metadata) Attributes(system, namespace string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{BindCountKey.Int(m.ArgumentCount)}
	if m.Summary != "" {
		attrs = append(attrs, QuerySummaryKey.String(m.Summary))
	}
	if system != "" {
		attrs = append(attrs, SystemKey.String(system))
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
	if m.QueryTextTruncated {
		attrs = append(attrs, QueryTextTruncatedKey.Bool(true))
	}
	return attrs
}

// Observer receives low-cardinality operation/result labels and elapsed time.
type Observer func(context.Context, string, string, time.Duration)

// SpanConfig configures a manually instrumented database/sql-style operation.
type SpanConfig struct {
	Tracer    trace.Tracer
	Namer     Namer
	Name      string
	Statement string
	Args      []any
	Dialect   Dialect
	System    string
	Namespace string
	Kind      trace.SpanKind
	Observe   Observer
}

// DBSpan is a manually instrumented database operation. End must be called
// exactly once after the database call finishes.
type DBSpan struct {
	span      trace.Span
	started   time.Time
	operation string
	observe   Observer
}

// StartSpan starts a database operation span with the same naming and safe SQL
// attributes used by the pgx and GORM adapters.
func StartSpan(ctx context.Context, config SpanConfig) (context.Context, *DBSpan) {
	if TracingDisabled(ctx) || config.Tracer == nil {
		return ctx, &DBSpan{}
	}
	name := NameFromContext(ctx)
	if name == "" {
		name = config.Name
	}
	meta := InspectDialect(config.Namer, name, config.Statement, config.Args, config.Dialect)
	kind := config.Kind
	if kind == trace.SpanKindUnspecified {
		kind = trace.SpanKindClient
	}
	ctx, span := config.Tracer.Start(ctx, meta.Name, trace.WithSpanKind(kind), trace.WithAttributes(meta.Attributes(config.System, config.Namespace)...))
	return ctx, &DBSpan{span: span, started: time.Now(), operation: meta.Operation, observe: config.Observe}
}

// End records rows affected, error classification, metrics, and ends the span.
// Pass a negative row count when it is unavailable.
func (s *DBSpan) End(ctx context.Context, err error, rows int64) {
	if s == nil || s.span == nil {
		return
	}
	if rows >= 0 {
		s.span.SetAttributes(attribute.Int64("db.response.returned_rows", rows))
	}
	result := "ok"
	if err != nil {
		result = "error"
		s.span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", err)))
	}
	gotel.End(s.span, err)
	if s.observe != nil {
		s.observe(ctx, s.operation, result, time.Since(s.started))
	}
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
