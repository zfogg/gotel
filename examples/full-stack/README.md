# Browser → Go → C, collected by Spaniel

This example follows one user action across three runtimes. A TypeScript page
instruments `fetch`, the Go API handles `POST /api/count`, and the API starts a
C word-count worker as a child process. The API passes W3C trace context to
the worker through `TRACEPARENT`. Each process exports OTLP telemetry to a
Spaniel container; Nginx forwards the browser's same-origin OTLP request to
that collector.

From the repository root, run:

```sh
docker compose -f examples/full-stack/compose.yml up --build --exit-code-from verify verify
```

Compose builds the Go and C binaries and the TypeScript page, starts Spaniel,
then runs a headless browser that clicks **Count words**. A separate verifier
container queries Spaniel and succeeds only when it finds the browser, Go HTTP
handler, Go worker-call, and C worker spans connected by parent IDs in one
trace. The runner passes the current request's trace ID to the verifier, so a
trace from an earlier run cannot satisfy the check. Its output includes a
direct link to that trace. The example page is at
<http://localhost:18081/> and the Spaniel UI is at
<http://localhost:18080/>.

The service names are `gotel-demo-web`, `gotel-demo-api`, and
`gotel-demo-worker`. Their spans show how the HTTP and process boundaries fit
together. The C worker also emits a correlated log and an operation metric.

When finished, stop the containers with:

```sh
docker compose -f examples/full-stack/compose.yml down
```

Docker Compose and an image-capable Docker installation are required. The
Spaniel image is `ghcr.io/zfogg/spaniel:latest`; use the same Compose command
to repeat the verification after editing the example.
