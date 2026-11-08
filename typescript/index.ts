import {
  FetchInstrumentation,
  type FetchInstrumentationConfig,
} from '@opentelemetry/instrumentation-fetch'
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http'
import { registerInstrumentations } from '@opentelemetry/instrumentation'
import { defaultResource, resourceFromAttributes, type Resource } from '@opentelemetry/resources'
import {
  BatchSpanProcessor,
  type BatchSpanProcessorBrowserConfig,
  type SpanExporter,
  type ReadableSpan,
} from '@opentelemetry/sdk-trace-base'
import { WebTracerProvider } from '@opentelemetry/sdk-trace-web'
import { trace, type Attributes, type Span } from '@opentelemetry/api'
import { W3CTraceContextPropagator } from '@opentelemetry/core'

/** Inputs used to produce a bounded, application-qualified HTTP operation name. */
export interface BrowserHttpSpanNameOptions {
  applicationOrigin: string
  method: string
  prefix: string
  responseUrl: string
}

/** Fetch instrumentation options with Gotel naming and origin classification. */
export type BrowserFetchInstrumentationOptions = Omit<
  FetchInstrumentationConfig,
  'applyCustomAttributesOnSpan' | 'requestHook'
> & {
  applicationOrigin?: string
  prefix: string
}

/** Name an HTTP client span without query strings, fragments or common path IDs. */
export function browserHttpClientSpanName({
  applicationOrigin,
  method,
  prefix,
  responseUrl,
}: BrowserHttpSpanNameOptions): string {
  const url = new URL(responseUrl)
  const path = boundedClientPath(url.pathname)
  const destination = url.origin === applicationOrigin ? path : `${url.host}${path}`
  return `${prefix ? `${prefix}.` : ""}http.client.${method.toUpperCase()} ${destination}`
}

function boundedClientPath(path: string): string {
  return path
    .split('/')
    .map((segment) =>
      /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(segment) ||
      /^[0-9a-f]{24,}$/i.test(segment) ||
      /^[0-9]+$/.test(segment)
        ? '{id}'
        : segment,
    )
    .join('/')
}

function privateUrlRemoved(value: string): string {
  try {
    const url = new URL(value)
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return '[redacted]'
    url.username = ''
    url.password = ''
    url.search = ''
    url.hash = ''
    return url.href
  } catch {
    return '[redacted]'
  }
}

function publicHttpAttributes(attributes: Attributes): Attributes {
  const output = { ...attributes }
  for (const key of ['url.full', 'http.url']) {
    if (typeof output[key] === 'string') output[key] = privateUrlRemoved(output[key])
  }
  for (const key of ['url.query', 'url.fragment']) delete output[key]
  if (typeof output['http.target'] === 'string') output['http.target'] = output['http.target'].split(/[?#]/, 1)[0]
  return output
}

/** Remove URL credentials, queries and fragments at export, including attributes
 * written after fetch hooks. Original spans and application requests stay intact.
 */
export function withoutPrivateHttpUrls(exporter: SpanExporter): SpanExporter {
  return {
    export(spans, callback) {
      exporter.export(spans.map((span): ReadableSpan => {
        const attributes = publicHttpAttributes(span.attributes)
        const events = span.events.map(event => ({ ...event, attributes: event.attributes ? publicHttpAttributes(event.attributes) : undefined }))
        return new Proxy(span, { get(target, property, receiver) {
          if (property === 'attributes') return attributes
          if (property === 'events') return events
          return Reflect.get(target, property, receiver)
        } })
      }), callback)
    },
    shutdown: () => exporter.shutdown(),
    forceFlush: () => exporter.forceFlush?.() ?? Promise.resolve(),
  }
}

/** Create standard OTel fetch instrumentation with bounded Gotel operation names. */
export function createBrowserFetchInstrumentation({
  applicationOrigin = globalThis.location.origin,
  prefix,
  ...config
}: BrowserFetchInstrumentationOptions): FetchInstrumentation {
  function name(span: Span, request: Request | RequestInit, responseUrl?: string) {
    // fetch(url, init) supplies RequestInit to hooks, so URL/method may only
    // exist on the SDK span. Request objects expose them directly.
    const attributes = (span as Span & { readonly attributes?: Attributes }).attributes ?? {}
    const url = responseUrl || (request instanceof Request ? request.url : attributes['url.full'] ?? attributes['http.url'])
    if (typeof url !== 'string' || !url) return
    for (const key of ['url.full', 'http.url']) {
      if (typeof attributes[key] === 'string') span.setAttribute(key, privateUrlRemoved(attributes[key]))
    }
    span.updateName(browserHttpClientSpanName({
      applicationOrigin,
      method: request.method ?? String(attributes['http.request.method'] ?? attributes['http.method'] ?? 'GET'),
      prefix,
      responseUrl: url,
    }))
  }
  return new FetchInstrumentation({
    ...config,
    requestHook(span, request) {
      name(span, request)
    },
    applyCustomAttributesOnSpan(span, request, result) {
      if (result instanceof Response) {
        name(span, request, result.url)
      }
    },
  })
}

/** Browser provider, resource, batching, exporter and fetch configuration. */
export interface BrowserTelemetryOptions {
  /** Omit prefix + "." from emitted span names; resources/scopes stay intact. */
  disableSpanNamePrefix?: boolean
  prefix: string
  serviceName: string
  serviceVersion?: string
  environment?: string
  attributes?: Record<string, string | number | boolean>
  applicationOrigin?: string
  resource?: Resource
  traceExportUrl?: string
  traceExporter?: SpanExporter
  batch?: BatchSpanProcessorBrowserConfig
  fetch?: Omit<BrowserFetchInstrumentationOptions, 'prefix' | 'applicationOrigin'>
}

/** Export names without one application prefix, preserving immutable input spans. */
export function withoutApplicationPrefix(exporter: SpanExporter, application: string): SpanExporter {
  const prefix = application.trim().replace(/\.+$/, '')
  return {
    export(spans, callback) {
      const output = spans.map((span): ReadableSpan => {
        const name = prefix && span.name.startsWith(prefix + '.') ? span.name.slice(prefix.length + 1) : span.name
        return new Proxy(span, { get(target, property, receiver) {
          return property === 'name' ? name || span.name : Reflect.get(target, property, receiver)
        } })
      })
      exporter.export(output, callback)
    },
    shutdown: () => exporter.shutdown(),
    forceFlush: () => exporter.forceFlush?.() ?? Promise.resolve(),
  }
}

/** Authentication state callback and the underlying application-owned exporter. */
export interface AuthenticatedExporterOptions {
  authenticated: () => boolean
  exporter: SpanExporter
}

/** Drops queued browser spans after logout. The server must still authenticate exports. */
export class AuthenticatedSpanExporter implements SpanExporter {
  constructor(private readonly options: AuthenticatedExporterOptions) {}
  /** Export while authenticated; otherwise complete successfully without sending. */
  export(spans: Parameters<SpanExporter['export']>[0], callback: Parameters<SpanExporter['export']>[1]) {
    if (!this.options.authenticated()) { callback({ code: 0 }); return }
    this.options.exporter.export(spans, callback)
  }
  shutdown() { return this.options.exporter.shutdown() }
  forceFlush() { return this.options.exporter.forceFlush?.() ?? Promise.resolve() }
}

/** Merge service identity, environment and explicit attributes with default resources. */
export function browserTelemetryResource(options: Pick<BrowserTelemetryOptions, 'serviceName' | 'serviceVersion' | 'environment' | 'attributes'>): Resource {
  return defaultResource().merge(resourceFromAttributes({
    'service.name': options.serviceName,
    ...(options.serviceVersion ? { 'service.version': options.serviceVersion } : {}),
    ...(options.environment ? { 'deployment.environment.name': options.environment } : {}),
    ...options.attributes,
  }))
}

const providers = new Map<string, WebTracerProvider>()
const unregister = new Map<string, () => void>()

/** Start browser resources, provider, batching and fetch instrumentation once per service.
 * Applications may supply traceExporter to retain exporter policy. Clean up with
 * shutdownBrowserTelemetry to unregister instrumentation and release the provider.
 */
export function startBrowserTelemetry({
  prefix,
  disableSpanNamePrefix = false,
  serviceName,
  serviceVersion,
  environment,
  attributes = {},
  applicationOrigin,
  resource,
  traceExportUrl = '/api/telemetry/v1/traces',
  traceExporter,
  batch,
  fetch = {},
}: BrowserTelemetryOptions): WebTracerProvider {
	const existing = providers.get(serviceName)
	if (existing) return existing
	const applicationResource = browserTelemetryResource({ serviceName, serviceVersion, environment, attributes })
  const originalExporter = withoutPrivateHttpUrls(traceExporter ?? new OTLPTraceExporter({ url: traceExportUrl }))
  const exporter = disableSpanNamePrefix ? withoutApplicationPrefix(originalExporter, prefix) : originalExporter
  const provider = new WebTracerProvider({
    resource: resource ? applicationResource.merge(resource) : applicationResource,
    spanProcessors: [new BatchSpanProcessor(exporter, batch)],
  })
  // Make propagation explicit instead of inheriting whatever global
  // propagator a host application happened to install. Without this, a
  // browser bundle can create client spans while omitting traceparent from
  // same-origin fetches, leaving the server span in a separate trace.
  provider.register({ propagator: new W3CTraceContextPropagator() })
  const remove = registerInstrumentations({
    tracerProvider: provider,
    instrumentations: [
      createBrowserFetchInstrumentation({ ...fetch, applicationOrigin, prefix }),
    ],
  })
	unregister.set(serviceName, remove)
	providers.set(serviceName, provider)
  return provider
}

/** Unregister fetch instrumentation and shut down the cached service provider. */
export async function shutdownBrowserTelemetry(serviceName: string): Promise<void> {
  const provider = providers.get(serviceName)
  if (!provider) return
  providers.delete(serviceName)
  unregister.get(serviceName)?.()
  unregister.delete(serviceName)
  const current = trace.getTracerProvider()
  if ('getDelegate' in current && typeof current.getDelegate === 'function' && current.getDelegate() === provider) {
    trace.disable()
    const replacement = providers.values().next().value
    if (replacement) trace.setGlobalTracerProvider(replacement)
  }
  await provider.shutdown()
}
