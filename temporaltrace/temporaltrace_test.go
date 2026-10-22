package temporaltrace

import "testing"

func TestNameIncludesWorkflowAndActivityType(t *testing.T) {
	for input, want := range map[string]string{
		"RunWorkflow:RefreshWorkflow":    "temporal.workflow.RefreshWorkflow",
		"RunActivity:Refresh":            "temporal.activity.Refresh",
		"SignalWorkflow:RefreshWorkflow": "temporal.signalworkflow.RefreshWorkflow",
	} {
		if got := Name(input); got != want {
			t.Errorf("Name(%q) = %q, want %q", input, got, want)
		}
	}
}
