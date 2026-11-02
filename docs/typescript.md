# Gotel TypeScript API

Browser OpenTelemetry initialization, resources, authenticated trace export and
bounded HTTP client operation names. The package is `@zfogg/gotel`.

```ts
import { startBrowserTelemetry, shutdownBrowserTelemetry } from '@zfogg/gotel'

const telemetry = startBrowserTelemetry({
  prefix: 'web',
  serviceName: 'my-app',
})
// On application shutdown:
await shutdownBrowserTelemetry('my-app')
```

Browser fetch instrumentation uses the configured application origin and
normalizes common identifier path segments. Query strings and fragments are
excluded from generated operation names. `disableSpanNamePrefix` changes span
names while preserving resources and instrumentation scopes.

Use `AuthenticatedSpanExporter` when an application's transport must add
authentication. See the exported option types for resource and batching controls.
