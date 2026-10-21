import { FetchInstrumentation, } from '@opentelemetry/instrumentation-fetch';
export function browserHttpClientSpanName({ applicationOrigin, method, prefix, responseUrl, }) {
    const url = new URL(responseUrl);
    const destination = url.origin === applicationOrigin ? url.pathname : `${url.host}${url.pathname}`;
    return `${prefix}.http.client.${method.toUpperCase()} ${destination}`;
}
function concreteFetchMethod(request) {
    if (!request.method) {
        throw new Error('fetch instrumentation request method was not normalized');
    }
    return request.method;
}
export function createBrowserFetchInstrumentation({ applicationOrigin = globalThis.location.origin, prefix, ...config }) {
    return new FetchInstrumentation({
        ...config,
        applyCustomAttributesOnSpan(span, request, result) {
            if (result instanceof Response) {
                span.updateName(browserHttpClientSpanName({
                    applicationOrigin,
                    method: concreteFetchMethod(request),
                    prefix,
                    responseUrl: result.url,
                }));
            }
        },
        requestHook(_span, request) {
            if (!(request instanceof Request) && !request.method) {
                request.method = 'GET';
            }
        },
    });
}
//# sourceMappingURL=index.js.map