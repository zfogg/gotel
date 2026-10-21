import { afterEach, describe, expect, it, vi } from 'vitest'

import { browserHttpClientSpanName, createBrowserFetchInstrumentation } from './index'

afterEach(() => vi.unstubAllGlobals())

describe('browserHttpClientSpanName', () => {
  it('uses only the path for same-origin requests', () => {
    expect(
      browserHttpClientSpanName({
        applicationOrigin: 'http://pi5:8081',
        method: 'get',
        prefix: 'stonkpile',
        responseUrl: 'http://pi5:8081/api/market-map?timeframe=5m',
      }),
    ).toBe('stonkpile.http.client.GET /api/market-map')
  })

  it('includes the host and non-default port for external requests', () => {
    expect(
      browserHttpClientSpanName({
        applicationOrigin: 'http://localhost:8085',
        method: 'POST',
        prefix: 'ratio',
        responseUrl: 'https://example.com:8443/orders?dry_run=true',
      }),
    ).toBe('ratio.http.client.POST example.com:8443/orders')
  })
})

describe('createBrowserFetchInstrumentation', () => {
  it('normalizes the browser default method once at the request boundary', () => {
    vi.stubGlobal('location', { origin: 'http://localhost:8085' })
    const instrumentation = createBrowserFetchInstrumentation({
      prefix: 'ratio',
    })
    const config = instrumentation.getConfig()
    const request: RequestInit = {}

    config.requestHook?.({} as never, request)

    expect(request.method).toBe('GET')
  })
})
