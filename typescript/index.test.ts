import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";

import { browserHttpClientSpanName, createBrowserFetchInstrumentation } from "./index";
import { WebTracerProvider } from '@opentelemetry/sdk-trace-web';

describe("browserHttpClientSpanName", () => {
  it("omits the separator when no application prefix is configured", () => {
    expect(browserHttpClientSpanName({
      prefix: "", method: "get", applicationOrigin: "https://frontend.example",
      responseUrl: "https://frontend.example/items/123?secret=value",
    })).toBe("http.client.GET /items/{id}");
  });
  it('names fetch(url, init) with default GET and names failed requests at start', () => {
    const instrumentation = createBrowserFetchInstrumentation({prefix:'demo',applicationOrigin:'https://frontend.example'});
    const provider = new WebTracerProvider();
    const span = provider.getTracer('test').startSpan('GET', {attributes:{'url.full':'https://api.example/users/123?secret=value','http.request.method':'GET'}});
    instrumentation.getConfig().requestHook?.(span, {});
    expect((span as unknown as {name:string}).name).toBe('demo.http.client.GET api.example/users/{id}');
    const response = new Response();
    Object.defineProperty(response,'url',{value:'https://api.example/users/456?secret=value'});
    instrumentation.getConfig().applyCustomAttributesOnSpan?.(span, {}, response);
    expect((span as unknown as {name:string}).name).toBe('demo.http.client.GET api.example/users/{id}');
    span.end();
    instrumentation.disable();
    return provider.shutdown();
  });
  it("matches the shared Go/C/TypeScript client naming corpus", () => {
    const fixture = readFileSync(new URL('../testdata/http_names.tsv', import.meta.url), 'utf8')
    for (const line of fixture.split(/\r?\n/)) {
      if (!line || line.startsWith('#')) continue
      const [method, responseUrl, expected] = line.split('\t')
      expect(browserHttpClientSpanName({
        applicationOrigin: 'https://frontend.example', method, prefix: 'demo', responseUrl,
      })).toBe(expected)
    }
  })
  it("uses the project HTTP client convention and excludes query cardinality", () => {
    expect(
      browserHttpClientSpanName({
        applicationOrigin: "http://pi5:8081",
        method: "get",
        prefix: "stonkpile",
        responseUrl: "http://pi5:8081/api/market-map?timeframe=5m",
      }),
    ).toBe("stonkpile.http.client.GET /api/market-map");
  });

  it("includes the destination host for external requests", () => {
    expect(
      browserHttpClientSpanName({
        applicationOrigin: "http://pi5:8081",
        method: "POST",
        prefix: "stonkpile",
        responseUrl: "https://example.com/orders?dry_run=true",
      }),
    ).toBe("stonkpile.http.client.POST example.com/orders");
  });

  it("retains a non-default external port", () => {
    expect(
      browserHttpClientSpanName({
        applicationOrigin: "http://pi5:8081",
        method: "POST",
        prefix: "stonkpile",
        responseUrl: "https://example.com:8443/orders",
      }),
    ).toBe("stonkpile.http.client.POST example.com:8443/orders");
  });

  it("bounds resource identifiers in same-origin and external paths", () => {
    expect(
      browserHttpClientSpanName({
        applicationOrigin: "http://pi5:8081",
        method: "DELETE",
        prefix: "stonkpile",
        responseUrl: "http://pi5:8081/api/reports/a128a1ac-6537-4a9e-9280-0ef7c14c33e2",
      }),
    ).toBe("stonkpile.http.client.DELETE /api/reports/{id}");
    expect(
      browserHttpClientSpanName({
        applicationOrigin: "http://pi5:8081",
        method: "GET",
        prefix: "stonkpile",
        responseUrl: "https://example.com/runs/2460",
      }),
    ).toBe("stonkpile.http.client.GET example.com/runs/{id}");
  });
});


describe('optional application span prefix', () => {
  it('changes only exported names and preserves original spans and relationships', async () => {
    const { withoutApplicationPrefix } = await import('./index');
    const { InMemorySpanExporter, SimpleSpanProcessor } = await import('@opentelemetry/sdk-trace-base');
    const output = new InMemorySpanExporter();
    const provider = new WebTracerProvider({spanProcessors:[new SimpleSpanProcessor(withoutApplicationPrefix(output,'demo'))]});
    const span = provider.getTracer('demo/http').startSpan('demo.http.client.GET example.com/items');
    span.setAttribute('example', 7); span.end();
    await provider.forceFlush();
    const exported = output.getFinishedSpans()[0];
    expect(exported.name).toBe('http.client.GET example.com/items');
    expect(exported.attributes).toEqual({example:7});
    expect(exported.spanContext()).toEqual(span.spanContext());
    expect(exported.instrumentationScope.name).toBe('demo/http');
    expect((span as unknown as {name:string}).name).toBe('demo.http.client.GET example.com/items');
    await provider.shutdown();
  });
});
