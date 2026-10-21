import { describe, expect, it } from 'vitest'

import { browserHttpClientSpanName } from './index'

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
