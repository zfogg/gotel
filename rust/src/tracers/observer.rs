use crate::{operation::QueryMetrics, Application, Namer, NamingError, Operation};
use opentelemetry::{
    metrics::MeterProvider,
    trace::{SpanKind, TraceContextExt, Tracer, TracerProvider},
    Context, KeyValue,
};
use opentelemetry_sdk::trace::SdkTracer;

#[derive(Clone)]
pub(super) struct Observer {
    tracer: SdkTracer,
    metrics: QueryMetrics,
    pub names: Namer,
}
impl Observer {
    pub fn new(
        app: &Application,
        prefix: &str,
        duration: &'static str,
        count: &'static str,
    ) -> Result<Self, NamingError> {
        let meter = app.meter_provider().meter("gotel/adapters");
        Ok(Self {
            tracer: app.tracer_provider().tracer("gotel/adapters"),
            metrics: QueryMetrics {
                duration: meter.f64_histogram(duration).with_unit("s").build(),
                count: meter.u64_counter(count).build(),
            },
            names: Namer::new(prefix)?,
        })
    }
    pub fn start(
        &self,
        parent: &Context,
        name: String,
        kind: SpanKind,
        attributes: Vec<KeyValue>,
        labels: Vec<KeyValue>,
    ) -> Operation {
        let span = self
            .tracer
            .span_builder(name)
            .with_kind(kind)
            .with_attributes(attributes)
            .start_with_context(&self.tracer, parent);
        Operation::new(parent.with_span(span), Some(self.metrics.clone()), labels)
    }
}
