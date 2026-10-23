import { FetchInstrumentation, } from '@opentelemetry/instrumentation-fetch';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { registerInstrumentations } from '@opentelemetry/instrumentation';
import { defaultResource, resourceFromAttributes } from '@opentelemetry/resources';
import { BatchSpanProcessor, } from '@opentelemetry/sdk-trace-base';
import { WebTracerProvider } from '@opentelemetry/sdk-trace-web';
export function browserHttpClientSpanName({ applicationOrigin, method, prefix, responseUrl, }) {
    const url = new URL(responseUrl);
    const path = boundedClientPath(url.pathname);
    const destination = url.origin === applicationOrigin ? path : `${url.host}${path}`;
    return `${prefix}.http.client.${method.toUpperCase()} ${destination}`;
}
function boundedClientPath(path) {
    return path
        .split('/')
        .map((segment) => /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(segment) ||
        /^[0-9a-f]{24,}$/i.test(segment) ||
        /^[0-9]+$/.test(segment)
        ? '{id}'
        : segment)
        .join('/');
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
// AuthenticatedSpanExporter drops queued browser spans after logout while the
// server independently enforces authentication on every export.
export class AuthenticatedSpanExporter {
    options;
    constructor(options) {
        this.options = options;
    }
    export(spans, callback) {
        if (!this.options.authenticated()) {
            callback({ code: 0 });
            return;
        }
        this.options.exporter.export(spans, callback);
    }
    shutdown() { return this.options.exporter.shutdown(); }
    forceFlush() { return this.options.exporter.forceFlush?.() ?? Promise.resolve(); }
}
export function browserTelemetryResource(options) {
    return defaultResource().merge(resourceFromAttributes({
        'service.name': options.serviceName,
        ...(options.serviceVersion ? { 'service.version': options.serviceVersion } : {}),
        ...(options.environment ? { 'deployment.environment.name': options.environment } : {}),
        ...options.attributes,
    }));
}
const providers = new Map();
// startBrowserTelemetry owns the standard browser resource, provider, batch
// processor, and fetch instrumentation. Applications retain exporter policy by
// supplying traceExporter (for example, an authenticated exporter wrapper).
export function startBrowserTelemetry({ prefix, serviceName, serviceVersion, environment, attributes = {}, applicationOrigin, resource, traceExportUrl = '/api/telemetry/v1/traces', traceExporter, batch, fetch = {}, }) {
    const existing = providers.get(serviceName);
    if (existing)
        return existing;
    const applicationResource = browserTelemetryResource({ serviceName, serviceVersion, environment, attributes });
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
    providers.set(serviceName, provider);
    return provider;
}
export async function shutdownBrowserTelemetry(serviceName) {
    const provider = providers.get(serviceName);
    if (!provider)
        return;
    providers.delete(serviceName);
    await provider.shutdown();
}
//# sourceMappingURL=index.js.map