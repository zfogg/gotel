use opentelemetry::{
    trace::{Status, TraceContextExt},
    Context, KeyValue,
};

/// Explicit completion semantics. These affect telemetry, never driver results.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Outcome {
    /// Completed normally.
    Success,
    /// A genuine operation failure.
    Error,
    /// Caller cancellation; not a failure by default.
    Cancelled,
    /// Explicit resumable work-budget exhaustion; not a failure.
    Deferred,
}
impl Outcome {
    /// Stable attribute spelling.
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Success => "success",
            Self::Error => "error",
            Self::Cancelled => "cancelled",
            Self::Deferred => "deferred",
        }
    }
    /// Apply completion without ending a span or recording error messages.
    pub fn apply(self, context: &Context, error_type: Option<&'static str>) {
        let span = context.span();
        span.set_attribute(KeyValue::new("operation.outcome", self.as_str()));
        if self == Self::Error {
            span.set_status(Status::error(""));
            if let Some(kind) = error_type {
                span.set_attribute(KeyValue::new("error.type", kind));
            }
        } else {
            span.set_status(Status::Ok);
        }
        if self == Self::Deferred {
            span.add_event("work_budget_exhausted", Vec::new());
        }
    }
}
