import { afterEach, expect, it, vi } from 'vitest'
import { context, trace } from '@opentelemetry/api'
import { InMemorySpanExporter, SimpleSpanProcessor } from '@opentelemetry/sdk-trace-base'
import { WebTracerProvider } from '@opentelemetry/sdk-trace-web'

// The browser SDK checks this capability when its module is first evaluated.
vi.stubGlobal('PerformanceObserver', class {
  observe() {}
  disconnect() {}
})
vi.stubGlobal('location', new URL('https://frontend.example/'))
const { startBrowserTelemetry, shutdownBrowserTelemetry } = await import('./index')

afterEach(async () => {
  await shutdownBrowserTelemetry('browser-test')
  vi.unstubAllGlobals()
})

it('binds fetch to each new provider after shutdown and preserves request parenting', async () => {
  for (let run = 0; run < 2; run++) {
    const requests: Request[] = []
    vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init)
      requests.push(request)
      const response = new Response('ok')
      Object.defineProperty(response, 'url', { value: request.url })
      return response
    })
    const exporter = new InMemorySpanExporter()
    const provider = startBrowserTelemetry({
      serviceName:'browser-test', prefix:'test', applicationOrigin:'https://frontend.example',
      traceExporter:exporter, fetch:{propagateTraceHeaderCorsUrls:/.*/, ignoreNetworkEvents:true},
      batch:{scheduledDelayMillis:10},
    })
    const parent = trace.getTracer('action').startSpan('ui.Refresh')
    await context.with(trace.setSpan(context.active(), parent), async () => {
      await (await fetch('https://api.example/items/123?token=secret')).text()
    })
    parent.end()
    await vi.waitFor(async () => {
      await provider.forceFlush()
      expect(exporter.getFinishedSpans()).toHaveLength(2)
    }, {timeout:3000})
    const child = exporter.getFinishedSpans().find(span => span.name.startsWith('test.http.client.'))!
    expect(child.parentSpanContext?.spanId).toBe(parent.spanContext().spanId)
    expect(child.spanContext().traceId).toBe(parent.spanContext().traceId)
    expect(requests[0].headers.get('traceparent')?.split('-')[1]).toBe(parent.spanContext().traceId)
    expect(child.attributes['url.full']).toBe('https://api.example/items/123')
    expect(requests[0].url).toContain('token=secret')
    await shutdownBrowserTelemetry('browser-test')
  }
})

it('leaves an application-owned global provider registered during shutdown', async () => {
  const output = new InMemorySpanExporter()
  const external = new WebTracerProvider({spanProcessors:[new SimpleSpanProcessor(output)]})
  expect(trace.setGlobalTracerProvider(external)).toBe(true)
  vi.stubGlobal('fetch', async () => new Response('ok'))
  try {
    startBrowserTelemetry({serviceName:'browser-test', prefix:'test', applicationOrigin:'https://frontend.example', traceExporter:new InMemorySpanExporter()})
    await shutdownBrowserTelemetry('browser-test')
    trace.getTracer('external').startSpan('still-active').end()
    await external.forceFlush()
    expect(output.getFinishedSpans().map(span => span.name)).toEqual(['still-active'])
  } finally {
    trace.disable()
    await external.shutdown()
  }
})
