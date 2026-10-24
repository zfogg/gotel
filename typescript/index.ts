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
import type { Attributes, Span } from '@opentelemetry/api'

export interface BrowserHttpSpanNameOptions {
  applicationOrigin: string
  method: string
  prefix: string
  responseUrl: string
}

export type BrowserFetchInstrumentationOptions = Omit<
  FetchInstrumentationConfig,
  'applyCustomAttributesOnSpan' | 'requestHook'
> & {
  applicationOrigin?: string
  prefix: string
}

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

export interface AuthenticatedExporterOptions {
  authenticated: () => boolean
  exporter: SpanExporter
}

// AuthenticatedSpanExporter drops queued browser spans after logout while the
// server independently enforces authentication on every export.
export class AuthenticatedSpanExporter implements SpanExporter {
  constructor(private readonly options: AuthenticatedExporterOptions) {}
  export(spans: Parameters<SpanExporter['export']>[0], callback: Parameters<SpanExporter['export']>[1]) {
    if (!this.options.authenticated()) { callback({ code: 0 }); return }
    this.options.exporter.export(spans, callback)
  }
  shutdown() { return this.options.exporter.shutdown() }
  forceFlush() { return this.options.exporter.forceFlush?.() ?? Promise.resolve() }
}

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

// startBrowserTelemetry owns the standard browser resource, provider, batch
// processor, and fetch instrumentation. Applications retain exporter policy by
// supplying traceExporter (for example, an authenticated exporter wrapper).
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
  const originalExporter = traceExporter ?? new OTLPTraceExporter({ url: traceExportUrl })
  const exporter = disableSpanNamePrefix ? withoutApplicationPrefix(originalExporter, prefix) : originalExporter
  const provider = new WebTracerProvider({
    resource: resource ? applicationResource.merge(resource) : applicationResource,
    spanProcessors: [new BatchSpanProcessor(exporter, batch)],
  })
  provider.register()
  const remove = registerInstrumentations({
    instrumentations: [
      createBrowserFetchInstrumentation({ ...fetch, applicationOrigin, prefix }),
    ],
  })
	unregister.set(serviceName, remove)
	providers.set(serviceName, provider)
  return provider
}

export async function shutdownBrowserTelemetry(serviceName: string): Promise<void> {
  const provider = providers.get(serviceName)
  if (!provider) return
  providers.delete(serviceName)
  unregister.get(serviceName)?.()
  unregister.delete(serviceName)
  await provider.shutdown()
}
