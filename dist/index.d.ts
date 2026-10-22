import { FetchInstrumentation, type FetchInstrumentationConfig } from '@opentelemetry/instrumentation-fetch';
import { type Resource } from '@opentelemetry/resources';
import { type BatchSpanProcessorBrowserConfig, type SpanExporter } from '@opentelemetry/sdk-trace-base';
import { WebTracerProvider } from '@opentelemetry/sdk-trace-web';
export interface BrowserHttpSpanNameOptions {
    applicationOrigin: string;
    method: string;
    prefix: string;
    responseUrl: string;
}
export type BrowserFetchInstrumentationOptions = Omit<FetchInstrumentationConfig, 'applyCustomAttributesOnSpan' | 'requestHook'> & {
    applicationOrigin?: string;
    prefix: string;
};
export declare function browserHttpClientSpanName({ applicationOrigin, method, prefix, responseUrl, }: BrowserHttpSpanNameOptions): string;
export declare function createBrowserFetchInstrumentation({ applicationOrigin, prefix, ...config }: BrowserFetchInstrumentationOptions): FetchInstrumentation;
export interface BrowserTelemetryOptions {
    prefix: string;
    serviceName: string;
    serviceVersion?: string;
    environment?: string;
    attributes?: Record<string, string | number | boolean>;
    applicationOrigin?: string;
    resource?: Resource;
    traceExportUrl?: string;
    traceExporter?: SpanExporter;
    batch?: BatchSpanProcessorBrowserConfig;
    fetch?: Omit<BrowserFetchInstrumentationOptions, 'prefix' | 'applicationOrigin'>;
}
export interface AuthenticatedExporterOptions {
    authenticated: () => boolean;
    exporter: SpanExporter;
}
export declare class AuthenticatedSpanExporter implements SpanExporter {
    private readonly options;
    constructor(options: AuthenticatedExporterOptions);
    export(spans: Parameters<SpanExporter['export']>[0], callback: Parameters<SpanExporter['export']>[1]): void;
    shutdown(): Promise<void>;
    forceFlush(): Promise<void>;
}
export declare function browserTelemetryResource(options: Pick<BrowserTelemetryOptions, 'serviceName' | 'serviceVersion' | 'environment' | 'attributes'>): Resource;
export declare function startBrowserTelemetry({ prefix, serviceName, serviceVersion, environment, attributes, applicationOrigin, resource, traceExportUrl, traceExporter, batch, fetch, }: BrowserTelemetryOptions): WebTracerProvider;
export declare function shutdownBrowserTelemetry(serviceName: string): Promise<void>;
//# sourceMappingURL=index.d.ts.map