package redistrace

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zfogg/gotel/go"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestCommandNameIsBoundedAndNeverContainsArguments(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) }()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: time.Millisecond, Dialer: func(context.Context, string, string) (net.Conn, error) { return nil, context.DeadlineExceeded }})
	client.AddHook(New(Config{Application: gotel.NewApplication("ratio")}))
	_ = client.SScan(t.Context(), "secret-key", 0, "secret-pattern", 1).Err()
	if got := recorder.Ended()[0].Name(); got != "storage.redis.SSCAN" {
		t.Fatalf("span name = %q", got)
	}
}

func TestArgsSizeCountsPayloadWithoutReturningValues(t *testing.T) {
	args := []interface{}{"SET", "bt:report:secret-report-id", []byte("payload")}
	if got, want := argsSize(args), len("SET")+len("bt:report:secret-report-id")+len("payload"); got != want {
		t.Fatalf("argsSize = %d, want %d", got, want)
	}
}

func TestScriptCacheMissIsNotReportedAsAnOperationFailure(t *testing.T) {
	if err := traceError(redis.ErrNoScript); err != nil {
		t.Fatalf("trace error = %v, want nil", err)
	}
}

func TestHookReturnsChainErrorWhenCommandHasNoError(t *testing.T) {
	h := New(Config{Application: gotel.NewApplication("ratio")})
	process := h.ProcessHook(func(context.Context, redis.Cmder) error {
		return redis.ErrNoScript
	})
	cmd := redis.NewCmd(t.Context(), "evalsha", "digest", 0)
	if err := process(t.Context(), cmd); err != redis.ErrNoScript {
		t.Fatalf("process error = %v, want %v", err, redis.ErrNoScript)
	}
	if cmd.Err() != nil {
		t.Fatalf("fixture command unexpectedly has error %v", cmd.Err())
	}
}

func TestDisableRootSpansKeepsObservationsAndTracesChildren(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) }()
	observed := 0
	h := New(Config{
		Application:      gotel.NewApplication("stonkpile"),
		DisableRootSpans: true,
		Observe:          func(context.Context, Observation) { observed++ },
	})
	process := h.ProcessHook(func(context.Context, redis.Cmder) error { return nil })
	cmd := redis.NewCmd(t.Context(), "GET", "secret")
	if err := process(t.Context(), cmd); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Ended()) != 0 || observed != 1 {
		t.Fatalf("root spans = %d, observations = %d; want 0, 1", len(recorder.Ended()), observed)
	}
	ctx, parent := provider.Tracer("test").Start(t.Context(), "stonkpile.operation")
	if err := process(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	parent.End()
	var childParent trace.SpanID
	for _, span := range recorder.Ended() {
		if span.Name() == "storage.redis.GET" {
			childParent = span.Parent().SpanID()
		}
	}
	if childParent != parent.SpanContext().SpanID() || observed != 2 {
		t.Fatalf("child parent = %s, observations = %d; want %s, 2", childParent, observed, parent.SpanContext().SpanID())
	}
}
