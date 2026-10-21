import { FetchInstrumentation, } from '@opentelemetry/instrumentation-fetch';
export function browserHttpClientSpanName({ applicationOrigin, method, prefix, responseUrl, }) {
    const url = new URL(responseUrl);
    const destination = url.origin === applicationOrigin ? url.pathname : `${url.host}${url.pathname}`;
    return `${prefix}.http.client.${method.toUpperCase()} ${destination}`;
}
export function createBrowserFetchInstrumentation({ applicationOrigin = globalThis.location.origin, prefix, ...config }) {
    return new FetchInstrumentation({
        ...config,
        applyCustomAttributesOnSpan(span, request, result) {
            if (result instanceof Response) {
                span.updateName(browserHttpClientSpanName({
                    applicationOrigin,
                    method: request.method,
                    prefix,
                    responseUrl: result.url,
                }));
            }
        },
    });
}
//# sourceMappingURL=index.js.map