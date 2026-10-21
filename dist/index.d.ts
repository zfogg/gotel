import { FetchInstrumentation, type FetchInstrumentationConfig } from '@opentelemetry/instrumentation-fetch';
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
//# sourceMappingURL=index.d.ts.map