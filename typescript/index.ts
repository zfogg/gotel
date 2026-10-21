import {
  FetchInstrumentation,
  type FetchInstrumentationConfig,
} from '@opentelemetry/instrumentation-fetch'

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
