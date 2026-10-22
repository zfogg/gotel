package temporaltrace

import (
	"testing"

	"github.com/zfogg/gotel"
	"go.temporal.io/sdk/client"
)

func TestNameIncludesWorkflowAndActivityType(t *testing.T) {
	for input, want := range map[string]string{
		"RunWorkflow:RefreshWorkflow":    "temporal.workflow.RefreshWorkflow",
		"StartActivity:Refresh":          "temporal.activity.schedule.Refresh",
		"RunActivity:Refresh":            "temporal.activity.execute.Refresh",
		"SignalWorkflow:RefreshWorkflow": "temporal.signalworkflow.RefreshWorkflow",
	} {
		if got := Name(input); got != want {
			t.Errorf("Name(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestConfigureClientInstallsCombinedInterceptorOnce(t *testing.T) {
	options, err := ConfigureClient(client.Options{}, Config{Application: gotel.NewApplication("ratio")})
	if err != nil {
		t.Fatalf("ConfigureClient: %v", err)
	}
	if got := len(options.Interceptors); got != 1 {
		t.Fatalf("client interceptor count = %d, want 1", got)
	}
}
