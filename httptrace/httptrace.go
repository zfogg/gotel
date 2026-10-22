// Package httptrace provides application-prefixed HTTP span names without
// depending on an HTTP instrumentation library or router.
package httptrace

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Namer shares one naming policy between clients, servers and routers. Prefix
// is a stable application identifier, such as "stonkpile", "ratio" or "spaniel".
// The zero value produces names starting with "http.".
type Namer struct{ Prefix string }

// Transport instruments outbound HTTP with the shared naming convention.
func (n Namer) Transport(base http.RoundTripper, options ...otelhttp.Option) http.RoundTripper {
	options = append([]otelhttp.Option{otelhttp.WithSpanNameFormatter(n.ClientSpanName)}, options...)
	return otelhttp.NewTransport(base, options...)
}

// Handler instruments inbound HTTP and finalizes the route name after routing.
func (n Namer) Handler(next http.Handler, operation string, options ...otelhttp.Option) http.Handler {
	options = append([]otelhttp.Option{otelhttp.WithSpanNameFormatter(n.ServerSpanName)}, options...)
	return otelhttp.NewHandler(n.RouteMiddleware(nil)(next), operation, options...)
}

// ClientSpanName plugs directly into otelhttp.WithSpanNameFormatter. It keeps
// the destination host (including its port) and escaped path, omitting URL
// credentials, query parameters and fragments. It does not modify the request
// or any HTTP attributes. Every method, including extension methods, is retained.
func (n Namer) ClientSpanName(_ string, r *http.Request) string {
	target := ""
	if r.URL != nil {
		target = r.URL.Host + boundedClientPath(r.URL.EscapedPath())
	}
	return n.name("client", r.Method, target)
}

var (
	clientUUIDSegment = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	clientHexSegment  = regexp.MustCompile(`(?i)^[0-9a-f]{24,}$`)
	clientIntSegment  = regexp.MustCompile(`^[0-9]+$`)
)

// boundedClientPath keeps stable endpoint segments while replacing common
// resource identifiers. HTTP clients do not have router templates, so raw
// UUIDs, long hashes, and integer IDs would otherwise create one span name per
// resource and violate the same cardinality contract enforced for servers.
func boundedClientPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if clientUUIDSegment.MatchString(part) || clientHexSegment.MatchString(part) || clientIntSegment.MatchString(part) {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

// ServerSpanName uses Go's resolved ServeMux pattern. Unmatched or unresolved
// requests use "unknown", never a raw URL containing per-request identifiers.
// Routers with their own route storage can use RouteMiddleware or SetServerRoute.
func (n Namer) ServerSpanName(_ string, r *http.Request) string {
	return n.name("server", r.Method, standardRoute(r))
}

// SetServerRoute renames the existing server span and records its route template.
// It neither creates a second span nor changes its status or other attributes.
func (n Namer) SetServerRoute(ctx context.Context, method, route string) {
	span := trace.SpanFromContext(ctx)
	span.SetName(n.name("server", method, route))
	if route != "" {
		span.SetAttributes(attribute.String("http.route", route))
	}
}

// RouteMiddleware finalizes the active server span after the handler resolves
// its route. Install it inside the instrumentation middleware. A nil resolver
// uses Request.Pattern; other routers only supply a template lookup callback.
func (n Namer) RouteMiddleware(resolve func(*http.Request) string) func(http.Handler) http.Handler {
	if resolve == nil {
		resolve = standardRoute
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() { n.SetServerRoute(r.Context(), r.Method, resolve(r)) }()
			next.ServeHTTP(w, r)
		})
	}
}

func (n Namer) name(role, method, target string) string {
	if role == "server" && target == "" {
		target = "unknown"
	}
	prefix := strings.TrimSuffix(n.Prefix, ".")
	if prefix != "" {
		prefix += "."
	}
	return strings.TrimSpace(prefix + "http." + role + "." + method + " " + target)
}

func standardRoute(r *http.Request) string {
	pattern := r.Pattern
	if _, route, ok := strings.Cut(pattern, " "); ok {
		pattern = route // Strip ServeMux's optional method.
	}
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		pattern = pattern[i:] // Strip an optional registered host.
	}
	return pattern
}
