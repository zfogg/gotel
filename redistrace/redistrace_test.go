package redistrace

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zfogg/gotel"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	if got := recorder.Ended()[0].Name(); got != "ratio.storage.redis.sscan" {
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
