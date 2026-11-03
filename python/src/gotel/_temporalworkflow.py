"""Optional replay-aware workflow metadata around Temporal's existing spans."""

from temporalio import workflow
from temporalio.contrib.opentelemetry import TracingWorkflowInboundInterceptor


class WorkflowInterceptor(TracingWorkflowInboundInterceptor):
    def _completed_span(self, span_name, *args, additional_attributes=None, **kwargs):
        # The SDK owns replay checks, deterministic clocks, propagation and span
        # completion. Add metadata to its existing observation, never create one.
        if span_name.startswith("RunWorkflow:"):
            info = workflow.info()
            attrs = dict(additional_attributes or {})
            attrs.update(
                {
                    "temporal.workflow.attempt": info.attempt,
                    "temporal.workflow.retry": info.attempt > 1,
                    "temporal.workflow.id": info.workflow_id,
                    "temporal.workflow.run_id": info.run_id,
                }
            )
            additional_attributes = attrs
        return super()._completed_span(span_name, *args, additional_attributes=additional_attributes, **kwargs)
