// Package gotel provides application-owned OpenTelemetry naming, spans,
// correlation and shutdown conventions on top of the standard Go SDK.
//
// Subpackages adapt HTTP, SQL, Redis, Temporal, MCP and structured logging.
// The otelhost package owns resource detection, exporters and provider lifecycle;
// testkit supplies deterministic in-memory providers for instrumentation tests.
// SQL processing is shared with github.com/zfogg/sequel/go.
package gotel
