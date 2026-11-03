use crate::{Namer, NamingError, Operation};
#[cfg(feature = "otlp")]
use opentelemetry::KeyValue;
use opentelemetry::{
    metrics::MeterProvider,
    trace::{SpanKind, TraceContextExt, Tracer, TracerProvider},
    Context,
};
use opentelemetry_sdk::{
    logs::SdkLoggerProvider, metrics::SdkMeterProvider, trace::SdkTracerProvider,
};

/// Owned SDK providers. Cloning provider handles is safe; shutdown is explicit.
/// Construction never replaces OpenTelemetry's global providers or propagator.
pub struct Application {
    tracer: SdkTracerProvider,
    meter: SdkMeterProvider,
    logger: SdkLoggerProvider,
    names: Namer,
}
impl Application {
    /// Wrap application-supplied providers, including test/in-memory exporters.
    pub fn with_providers(
        name: impl Into<String>,
        tracer: SdkTracerProvider,
        meter: SdkMeterProvider,
        logger: SdkLoggerProvider,
    ) -> Result<Self, NamingError> {
        Ok(Self {
            tracer,
            meter,
            logger,
            names: Namer::new(name)?,
        })
    }
    /// Access this application's tracer provider.
    pub fn tracer_provider(&self) -> &SdkTracerProvider {
        &self.tracer
    }
    /// Access native meter APIs for counters, histograms and gauges.
    pub fn meter_provider(&self) -> &SdkMeterProvider {
        &self.meter
    }
    /// Access native structured logging APIs. Callers select safe log bodies.
    pub fn logger_provider(&self) -> &SdkLoggerProvider {
        &self.logger
    }
    /// Start a source-named operation with explicit causal parentage.
    pub fn operation(
        &self,
        parent: &Context,
        name: &'static str,
        kind: SpanKind,
    ) -> Result<Operation, NamingError> {
        let tracer = self.tracer.tracer("gotel");
        let span = tracer
            .span_builder(self.names.name(name)?)
            .with_kind(kind)
            .start_with_context(&tracer, parent);
        Ok(Operation::new(parent.with_span(span), None, Vec::new()))
    }
    /// Create a SQL tracer using these providers and a validated database namespace.
    pub fn database(
        &self,
        prefix: impl Into<String>,
        dialect: sequel::Dialect,
        namespace: impl Into<String>,
    ) -> Result<crate::SqlTracer, NamingError> {
        Ok(crate::SqlTracer::new(
            self.tracer.tracer("gotel/sql"),
            self.meter.meter("gotel/sql"),
            Namer::new(prefix)?,
            dialect,
            namespace.into(),
        ))
    }
    /// Flush all owned signals, even if one signal fails. No global state is changed.
    pub fn force_flush(&self) -> opentelemetry_sdk::error::OTelSdkResult {
        let traces = self.tracer.force_flush();
        let metrics = self.meter.force_flush();
        let logs = self.logger.force_flush();
        traces.and(metrics).and(logs)
    }
    /// Shut down all owned signals. This application must no longer start work.
    pub fn shutdown(&self) -> opentelemetry_sdk::error::OTelSdkResult {
        let traces = self.tracer.shutdown();
        let metrics = self.meter.shutdown();
        let logs = self.logger.shutdown();
        traces.and(metrics).and(logs)
    }
}

/// OTLP/HTTP configuration. Exporters also honor the standard OTEL environment.
#[cfg(feature = "otlp")]
pub struct ApplicationConfig {
    /// Source-owned service identity and operation prefix.
    pub service_name: String,
    /// Optional base HTTP(S) endpoint. Signal paths are appended, preserving prefixes.
    pub endpoint: Option<String>,
    /// Explicit resources override detected attributes. Do not include secrets.
    pub resources: Vec<KeyValue>,
    /// Bypass environment proxies, useful for an explicitly local collector.
    pub bypass_proxy: bool,
}
#[cfg(feature = "otlp")]
impl ApplicationConfig {
    /// Start from SDK environment defaults without altering process environment.
    pub fn new(service_name: impl Into<String>) -> Self {
        Self {
            service_name: service_name.into(),
            endpoint: None,
            resources: Vec::new(),
            bypass_proxy: false,
        }
    }
}

/// Configuration/build failure; underlying errors are returned to the caller,
/// never recorded as telemetry exception messages.
#[cfg(feature = "otlp")]
pub type InitError = Box<dyn std::error::Error + Send + Sync>;

#[cfg(feature = "otlp")]
impl Application {
    /// Build owned HTTP/protobuf trace, metric and log pipelines. No Tokio runtime
    /// is required: batch/periodic exporters use SDK workers and a blocking client.
    pub fn otlp(config: ApplicationConfig) -> Result<Self, InitError> {
        use opentelemetry_otlp::{WithExportConfig, WithHttpConfig};
        let names = Namer::new(config.service_name.clone())?;
        let mut client = reqwest::blocking::Client::builder();
        if config.bypass_proxy {
            client = client.no_proxy();
        }
        let client = client.build()?;
        let resource = opentelemetry_sdk::Resource::builder()
            .with_service_name(config.service_name)
            .with_attributes([
                KeyValue::new("process.pid", i64::from(std::process::id())),
                KeyValue::new(
                    "os.type",
                    match std::env::consts::OS {
                        "macos" => "darwin",
                        other => other,
                    },
                ),
                KeyValue::new(
                    "host.arch",
                    match std::env::consts::ARCH {
                        "x86_64" => "amd64",
                        "aarch64" => "arm64",
                        other => other,
                    },
                ),
            ])
            .with_attributes(config.resources)
            .build();
        let mut traces = opentelemetry_otlp::SpanExporter::builder()
            .with_http()
            .with_http_client(client.clone());
        let mut metrics = opentelemetry_otlp::MetricExporter::builder()
            .with_http()
            .with_http_client(client.clone());
        let mut logs = opentelemetry_otlp::LogExporter::builder()
            .with_http()
            .with_http_client(client);
        if let Some(base) = config.endpoint {
            traces = traces.with_endpoint(signal_endpoint(&base, "traces")?);
            metrics = metrics.with_endpoint(signal_endpoint(&base, "metrics")?);
            logs = logs.with_endpoint(signal_endpoint(&base, "logs")?);
        }
        // Build exporters before starting any provider workers; a configuration
        // failure cannot leave a half-created provider running in the background.
        let trace_exporter = traces.build()?;
        let metric_exporter = metrics.build()?;
        let log_exporter = logs.build()?;
        let tracer = SdkTracerProvider::builder()
            .with_resource(resource.clone())
            .with_batch_exporter(trace_exporter)
            .build();
        let meter = SdkMeterProvider::builder()
            .with_resource(resource.clone())
            .with_periodic_exporter(metric_exporter)
            .build();
        let logger = SdkLoggerProvider::builder()
            .with_resource(resource)
            .with_batch_exporter(log_exporter)
            .build();
        Ok(Self {
            tracer,
            meter,
            logger,
            names,
        })
    }
}

#[cfg(feature = "otlp")]
fn signal_endpoint(base: &str, signal: &str) -> Result<String, InitError> {
    let mut endpoint = url::Url::parse(base)?;
    if !matches!(endpoint.scheme(), "http" | "https")
        || endpoint.query().is_some()
        || endpoint.fragment().is_some()
    {
        return Err("OTLP base endpoint must be HTTP(S) without a query or fragment".into());
    }
    endpoint.set_path(&format!(
        "{}/v1/{signal}",
        endpoint.path().trim_end_matches('/')
    ));
    Ok(endpoint.into())
}
