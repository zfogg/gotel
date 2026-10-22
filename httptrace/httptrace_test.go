package httptrace

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestServerSpanNameIsBoundedAndPrefixed(t *testing.T) {
	for _, prefix := range []string{"stonkpile", "ratio", "spaniel", ""} {
		n := Namer{Prefix: prefix}
		base := prefix
		if base != "" {
			base += "."
		}
		for _, tc := range []struct{ pattern, target string }{
			{"", "unknown"},
			{"/items/{id}", "/items/{id}"},
			{"GET /items/{id}", "/items/{id}"},
			{"GET example.com/items/{id}", "/items/{id}"},
		} {
			r := httptest.NewRequest(http.MethodGet, "/items/private?token=secret", nil)
			r.Pattern = tc.pattern
			if got, want := n.ServerSpanName("ignored", r), base+"http.server.GET "+tc.target; got != want {
				t.Errorf("prefix %q pattern %q: got %q, want %q", prefix, tc.pattern, got, want)
			}
		}
	}
}

func TestServeMuxAllMethodsKeepOneSpanAndParent(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "false")
	n := Namer{Prefix: "example"}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer provider.Shutdown(context.Background())
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := otelhttp.NewHandler(n.RouteMiddleware(nil)(mux), "example",
		otelhttp.WithTracerProvider(provider), otelhttp.WithSpanNameFormatter(n.ServerSpanName))
	methods := []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace, "PURGE"}
	for _, method := range methods {
		r := httptest.NewRequest(method, "/items/private?token=secret", nil)
		r = r.WithContext(trace.ContextWithSpanContext(r.Context(), parent))
		handler.ServeHTTP(httptest.NewRecorder(), r)
	}
	spans := recorder.Ended()
	if len(spans) != len(methods) {
		t.Fatalf("got %d spans for %d requests", len(spans), len(methods))
	}
	for i, span := range spans {
		if span.Name() != "example.http.server."+methods[i]+" /items/{id}" {
			t.Errorf("span name = %q", span.Name())
		}
		if span.Parent().SpanID() != parent.SpanID() || span.SpanContext().TraceID() != parent.TraceID() || span.SpanKind() != trace.SpanKindServer {
			t.Errorf("lost server kind or parent: %v", span)
		}
		found := false
		for _, attr := range span.Attributes() {
			if attr.Key == "http.route" && attr.Value.AsString() == "/items/{id}" {
				found = true
			}
		}
		if !found {
			t.Error("missing resolved route")
		}
	}
}

func TestRouteMiddlewareSupportsOtherRoutersWithoutWrappingWriter(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer provider.Shutdown(context.Background())
	ctx, span := provider.Tracer("independent-instrumentation").Start(t.Context(), "before", trace.WithSpanKind(trace.SpanKindServer))
	response := httptest.NewRecorder()
	n := Namer{Prefix: "other"}
	resolved := ""
	handler := n.RouteMiddleware(func(*http.Request) string { return resolved })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w != response || trace.SpanFromContext(r.Context()) != span {
			t.Fatal("writer or span was replaced")
		}
		resolved = "/custom/{id}"
	}))
	handler.ServeHTTP(response, httptest.NewRequest("PURGE", "/custom/private", nil).WithContext(ctx))
	span.End()
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "other.http.server.PURGE /custom/{id}" {
		t.Fatalf("spans = %v", spans)
	}
}

func TestHTTPClientPrefixPreservesDestination(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "false")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer provider.Shutdown(context.Background())
	for _, prefixed := range []bool{false, true} {
		opts := []otelhttp.Option{otelhttp.WithTracerProvider(provider)}
		if prefixed {
			opts = append(opts, otelhttp.WithSpanNameFormatter((Namer{Prefix: "stonkpile"}).ClientSpanName))
		}
		client := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport, opts...)}
		response, err := client.Get(server.URL + "/original/destination?original=value")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want two", len(spans))
	}
	if spans[1].Name() != "stonkpile.http.client.GET "+strings.TrimPrefix(server.URL, "http://")+"/original/destination" {
		t.Fatalf("span name = %q; original = %q", spans[1].Name(), spans[0].Name())
	}
	attrs := func(span sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
		result := make(map[attribute.Key]attribute.Value)
		for _, attr := range span.Attributes() {
			result[attr.Key] = attr.Value
		}
		return result
	}
	if !reflect.DeepEqual(attrs(spans[0]), attrs(spans[1])) {
		t.Fatal("prefix changed HTTP attributes")
	}
	if spans[1].SpanKind() != trace.SpanKindClient {
		t.Fatal("outgoing span lost client kind")
	}
}

func TestClientSpanNameAllMethods(t *testing.T) {
	u, err := url.Parse("https://user:secret@example.com:8443/a%2Fb?token=secret#fragment")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace, "PURGE"} {
		t.Run(method, func(t *testing.T) {
			r := &http.Request{Method: method, URL: u}
			want := "stonkpile.http.client." + method + " example.com:8443/a%2Fb"
			if got := (Namer{Prefix: "stonkpile"}).ClientSpanName("", r); got != want {
				t.Fatalf("span name = %q, want %q", got, want)
			}
		})
	}
}

func TestClientSpanNameBoundsResourceIdentifiers(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://example.com/orders/a128a1ac-6537-4a9e-9280-0ef7c14c33e2", "example.com/orders/{id}"},
		{"https://example.com/reports/2360f5747fbf826f6e5ccde6376418286a886fcf2032122eb24a75f351391af0", "example.com/reports/{id}"},
		{"https://example.com/runs/2460", "example.com/runs/{id}"},
		{"https://example.com/bars/5m", "example.com/bars/5m"},
	} {
		r := httptest.NewRequest(http.MethodDelete, tc.raw, nil)
		if got := (Namer{Prefix: "stonkpile"}).ClientSpanName("", r); got != "stonkpile.http.client.DELETE "+tc.want {
			t.Errorf("%s: got %q", tc.raw, got)
		}
	}
}
