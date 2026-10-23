import { context, propagation, trace, SpanKind } from '@opentelemetry/api'
import { browserHttpClientSpanName, startBrowserTelemetry } from '@zfogg/gotel'

const provider = startBrowserTelemetry({
  prefix: 'demo.web',
  serviceName: 'gotel-demo-web',
  traceExportUrl: '/telemetry/v1/traces',
  fetch: { ignoreUrls: [/\/telemetry\//, /\/api\/count$/] },
})

const button = document.querySelector<HTMLButtonElement>('#count')!
const result = document.querySelector<HTMLOutputElement>('#result')!
button.addEventListener('click', async () => {
  button.disabled = true
  result.textContent = 'Counting…'
  const span = trace.getTracer('gotel-demo-web').startSpan(
    browserHttpClientSpanName({
      applicationOrigin: location.origin,
      method: 'POST',
      prefix: 'demo.web',
      responseUrl: new URL('/api/count', location.origin).href,
    }),
    { kind: SpanKind.CLIENT },
  )
  try {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    propagation.inject(trace.setSpan(context.active(), span), headers)
    const response = await fetch('/api/count', {
      method: 'POST',
      headers,
      body: JSON.stringify({ text: 'one two three four' }),
    })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    const data: { words: number } = await response.json()
    result.textContent = `${data.words} words`
    span.end()
    await provider.forceFlush()
    result.dataset.state = 'done'
  } catch (error) {
    span.recordException(error as Error)
    span.end()
    result.textContent = String(error)
    result.dataset.state = 'error'
  } finally {
    button.disabled = false
  }
})
