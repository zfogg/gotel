package gotel

import (
	"context"
	"strings"

	"github.com/zfogg/gotel/httptrace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Component is a stable instrumentation-scope component shared by applications.
type Component string

const (
	ComponentHTTP       Component = "http"
	ComponentStorage    Component = "storage"
	ComponentTemporal   Component = "temporal"
	ComponentBackground Component = "background"
	ComponentOperations Component = "operations"
	ComponentRuntime    Component = "runtime"
)

// Application is the shared OpenTelemetry convention boundary for one app.
// Its zero value is valid, but NewApplication is preferred so names cannot
// accidentally drift between HTTP, storage, Temporal, and domain spans.
type Application struct{ prefix string }

func NewApplication(prefix string) Application {
	return Application{prefix: strings.TrimSuffix(strings.TrimSpace(prefix), ".")}
}

func (a Application) Prefix() string { return a.prefix }

func (a Application) Scope() Scope { return Scope{Prefix: a.prefix} }

func (a Application) Tracer(component string) trace.Tracer { return a.Scope().Tracer(component) }

func (a Application) ComponentTracer(component Component) trace.Tracer {
	return a.Tracer(string(component))
}

func (a Application) Start(ctx context.Context, component, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return a.Scope().Start(ctx, component, name, attrs...)
}

func (a Application) HTTP() httptrace.Namer { return httptrace.Namer{Prefix: a.prefix} }

func (a Application) StorageName(system, operation string) string {
	return qualify(a.prefix, "storage."+strings.ToLower(strings.TrimSpace(system))+"."+strings.ToLower(strings.TrimSpace(operation)))
}
