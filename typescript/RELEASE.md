# Using the compiled TypeScript package

This ZIP contains the built `@zfogg/gotel` browser package: `dist/index.js`,
its TypeScript declarations, `package.json`, and the license. It does not
contain the C or Go source. The full project guide is included as `PROJECT.md`.

Extract the ZIP, then install the extracted directory from your browser app:

```sh
npm install /absolute/path/to/gotel-typescript-vX.Y.Z
# Or: pnpm add /absolute/path/to/gotel-typescript-vX.Y.Z
```

Use the directory inside the ZIP, which contains `package.json`. npm does not
install this ZIP directly. Your app also needs the OpenTelemetry peer
dependencies listed in that file. The `exports` field points to the compiled
ES module and TypeScript declarations; no build step is needed for gotel.

Initialize the browser provider once in your application:

```ts
import { startBrowserTelemetry } from '@zfogg/gotel'

startBrowserTelemetry({
  prefix: 'web',
  serviceName: 'my-browser-app',
  traceExportUrl: '/api/telemetry/v1/traces',
  fetch: { ignoreUrls: [/\/api\/telemetry\//] },
})
```

The trace URL must accept OTLP/HTTP protobuf. See `PROJECT.md` for provider
options, an existing-provider integration, and the Go and C APIs. If publishing
to an npm registry is configured later, consumers can install the same package
by name instead of downloading this ZIP.
