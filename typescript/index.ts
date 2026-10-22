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
} from '@opentelemetry/sdk-trace-base'
import { WebTracerProvider } from '@opentelemetry/sdk-trace-web'

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
  return `${prefix}.http.client.${method.toUpperCase()} ${destination}`
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
  return new FetchInstrumentation({
    ...config,
    requestHook(span, request) {
      const typedRequest = request as Request
      span.updateName(
        browserHttpClientSpanName({
          applicationOrigin,
          method: typedRequest.method,
          prefix,
          responseUrl: typedRequest.url,
        }),
      )
    },
    applyCustomAttributesOnSpan(span, request, result) {
      if (result instanceof Response) {
        span.updateName(
          browserHttpClientSpanName({
            applicationOrigin,
            method: (request as Request).method,
            prefix,
            responseUrl: result.url,
          }),
        )
      }
    },
  })
}

export interface BrowserTelemetryOptions {
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

// startBrowserTelemetry owns the standard browser resource, provider, batch
// processor, and fetch instrumentation. Applications retain exporter policy by
// supplying traceExporter (for example, an authenticated exporter wrapper).
export function startBrowserTelemetry({
  prefix,
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
  const exporter = traceExporter ?? new OTLPTraceExporter({ url: traceExportUrl })
  const provider = new WebTracerProvider({
    resource: resource ? applicationResource.merge(resource) : applicationResource,
    spanProcessors: [new BatchSpanProcessor(exporter, batch)],
  })
  provider.register()
  registerInstrumentations({
    instrumentations: [
      createBrowserFetchInstrumentation({ ...fetch, applicationOrigin, prefix }),
    ],
  })
	providers.set(serviceName, provider)
  return provider
}

export async function shutdownBrowserTelemetry(serviceName: string): Promise<void> {
  const provider = providers.get(serviceName)
  if (!provider) return
  providers.delete(serviceName)
  await provider.shutdown()
}
