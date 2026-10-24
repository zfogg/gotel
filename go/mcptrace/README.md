# MCP telemetry

`mcptrace` instruments the official `github.com/modelcontextprotocol/go-sdk/mcp`
server. It records a server span, request counter, and duration histogram for each
received method, including tool-result errors and panics. Panics are rethrown.
The handler receives the span context, so database and other child spans retain
their parent.

```go
middleware, err := mcptrace.New(mcptrace.Options{
    Prefix: "catalog",
    Tools: []string{"lookup", "search"},
})
if err != nil {
    return err
}
server.AddReceivingMiddleware(middleware)
```

Import `github.com/zfogg/gotel/go/mcptrace`. Install middleware once per server.
With this prefix the instrumentation scope is `catalog/mcp`, spans are named
`catalog.mcp.tool/lookup` or `catalog.mcp.tools/list`, and metrics are
`catalog.mcp.requests` and `catalog.mcp.duration` (milliseconds). Empty prefix
uses `mcp.*` names and the `mcp` scope. Metrics have `method`, `tool`, and `result`
labels; unknown methods and unregistered tools become `other` and `unknown`.
The vocabulary is copied when middleware is constructed; reconstruct it when
the server's registered tools change.

Spans record `rpc.system`, `rpc.method`, `gen_ai.tool.name`, and `mcp.is_error`.
Errors use a fixed status message and `error.type=mcp_error`. Arguments, results,
request identifiers, exception messages, and panic payloads are never exported.
Original results and errors pass through unchanged. Explicit tracer/meter
providers are optional. `Options.Context` can add application context before
the span starts, for example to select an application's service resource;
exporter setup and service names remain application policy.
