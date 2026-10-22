import { FetchInstrumentation, } from '@opentelemetry/instrumentation-fetch';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { registerInstrumentations } from '@opentelemetry/instrumentation';
import { defaultResource, resourceFromAttributes } from '@opentelemetry/resources';
import { BatchSpanProcessor, } from '@opentelemetry/sdk-trace-base';
import { WebTracerProvider } from '@opentelemetry/sdk-trace-web';
export function browserHttpClientSpanName({ applicationOrigin, method, prefix, responseUrl, }) {
    const url = new URL(responseUrl);
    const destination = url.origin === applicationOrigin ? url.pathname : `${url.host}${url.pathname}`;
    return `${prefix}.http.client.${method.toUpperCase()} ${destination}`;
}
export function createBrowserFetchInstrumentation({ applicationOrigin = globalThis.location.origin, prefix, ...config }) {
    return new FetchInstrumentation({
        ...config,
        requestHook(span, request) {
            const typedRequest = request;
            span.updateName(browserHttpClientSpanName({
                applicationOrigin,
                method: typedRequest.method,
                prefix,
                responseUrl: typedRequest.url,
            }));
        },
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
// startBrowserTelemetry owns the standard browser resource, provider, batch
// processor, and fetch instrumentation. Applications retain exporter policy by
// supplying traceExporter (for example, an authenticated exporter wrapper).
export function startBrowserTelemetry({ prefix, serviceName, serviceVersion, environment, attributes = {}, applicationOrigin, resource, traceExportUrl = '/api/telemetry/v1/traces', traceExporter, batch, fetch = {}, }) {
    const applicationResource = defaultResource().merge(resourceFromAttributes({
        'service.name': serviceName,
        ...(serviceVersion ? { 'service.version': serviceVersion } : {}),
        ...(environment ? { 'deployment.environment.name': environment } : {}),
        ...attributes,
    }));
    const exporter = traceExporter ?? new OTLPTraceExporter({ url: traceExportUrl });
    const provider = new WebTracerProvider({
        resource: resource ? applicationResource.merge(resource) : applicationResource,
        spanProcessors: [new BatchSpanProcessor(exporter, batch)],
    });
    provider.register();
    registerInstrumentations({
        instrumentations: [
            createBrowserFetchInstrumentation({ ...fetch, applicationOrigin, prefix }),
        ],
    });
    return provider;
}
//# sourceMappingURL=index.js.map