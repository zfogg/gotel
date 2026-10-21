import { describe, expect, it } from "vitest";

import { browserHttpClientSpanName } from "./index";

describe("browserHttpClientSpanName", () => {
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
});
