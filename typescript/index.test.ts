import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";

import { browserHttpClientSpanName } from "./index";

describe("browserHttpClientSpanName", () => {
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
