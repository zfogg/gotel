use crate::Outcome;
use opentelemetry::{
    metrics::{Counter, Histogram},
    trace::TraceContextExt,
    Context, KeyValue,
};
use std::time::Instant;

pub(crate) struct QueryMetrics {
    pub duration: Histogram<f64>,
    pub count: Counter<u64>,
}

/// An explicitly parented operation. It owns no thread-local attachment, so it
/// can cross async suspension safely. Unfinished work is cancelled on drop;
/// unwinding records a failure without the panic message.
pub struct Operation {
    context: Context,
    started: Instant,
    metrics: Option<QueryMetrics>,
    labels: Vec<KeyValue>,
    finished: bool,
}
impl Operation {
    pub(crate) fn new(
        context: Context,
        metrics: Option<QueryMetrics>,
        labels: Vec<KeyValue>,
    ) -> Self {
        Self {
            context,
            started: Instant::now(),
            metrics,
            labels,
            finished: false,
        }
    }
    /// Explicit context for propagating to database/HTTP calls or spawned tasks.
    pub fn context(&self) -> &Context {
        &self.context
    }
    /// Finish once. Error type must be source-owned; no error message is accepted.
    pub fn finish(mut self, result: Outcome, error_type: Option<&'static str>) {
        self.complete(result, error_type);
    }
    fn complete(&mut self, result: Outcome, error_type: Option<&'static str>) {
        if self.finished {
            return;
        }
        result.apply(&self.context, error_type);
        if let Some(metrics) = &self.metrics {
            let mut labels = self.labels.clone();
            labels.push(KeyValue::new("operation.outcome", result.as_str()));
            metrics
                .duration
                .record(self.started.elapsed().as_secs_f64(), &labels);
            metrics.count.add(1, &labels);
        }
        self.context.span().end();
        self.finished = true;
    }
}
impl Drop for Operation {
    fn drop(&mut self) {
        let panicking = std::thread::panicking();
        self.complete(
            if panicking {
                Outcome::Error
            } else {
                Outcome::Cancelled
            },
            panicking.then_some("panic"),
        );
    }
}
