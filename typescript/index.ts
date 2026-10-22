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
  const destination = url.origin === applicationOrigin ? url.pathname : `${url.host}${url.pathname}`
  return `${prefix}.http.client.${method.toUpperCase()} ${destination}`
}

export function createBrowserFetchInstrumentation({
  applicationOrigin = globalThis.location.origin,
  prefix,
  ...config
}: BrowserFetchInstrumentationOptions): FetchInstrumentation {
  return new FetchInstrumentation({
    ...config,
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
  const applicationResource = defaultResource().merge(
    resourceFromAttributes({
      'service.name': serviceName,
      ...(serviceVersion ? { 'service.version': serviceVersion } : {}),
      ...(environment ? { 'deployment.environment.name': environment } : {}),
      ...attributes,
    }),
  )
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
  return provider
}
