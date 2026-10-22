// Package redistrace instruments go-redis without recording keys or values.
package redistrace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zfogg/gotel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Observation struct {
	Command      string
	Count        int
	Duration     time.Duration
	Err          error
	PayloadBytes int
}

type Config struct {
	Application      gotel.Application
	Observe          func(context.Context, Observation)
	DisableRootSpans bool // keep metrics but emit spans only beneath an active trace
}

type hook struct{ config Config }

func New(config Config) redis.Hook { return &hook{config: config} }

func (h *hook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *hook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		operation := strings.ToLower(cmd.Name())
		started := time.Now()
		if h.config.DisableRootSpans && !trace.SpanContextFromContext(ctx).IsValid() {
			err := next(ctx, cmd)
			h.observe(ctx, Observation{Command: operation, Count: 1, Duration: time.Since(started), Err: traceError(err), PayloadBytes: argsSize(cmd.Args())})
			return err
		}
		ctx, span := h.config.Application.Start(ctx, "redis", "storage.redis."+operation,
			attribute.String("db.system.name", "redis"),
			attribute.String("db.operation.name", operation),
		)
		err := next(ctx, cmd)
		traceErr := traceError(err)
		gotel.End(span, traceErr)
		h.observe(ctx, Observation{Command: operation, Count: 1, Duration: time.Since(started), Err: traceErr, PayloadBytes: argsSize(cmd.Args())})
		return err
	}
}

func traceError(err error) error {
	if errors.Is(err, redis.Nil) || errors.Is(err, redis.ErrNoScript) {
		return nil
	}
	return err
}

func argsSize(args []interface{}) int {
	size := 0
	for _, arg := range args {
		switch value := arg.(type) {
		case string:
			size += len(value)
		case []byte:
			size += len(value)
		default:
			size += len(fmt.Sprint(value))
		}
	}
	return size
}

func (h *hook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		started := time.Now()
		if h.config.DisableRootSpans && !trace.SpanContextFromContext(ctx).IsValid() {
			err := next(ctx, cmds)
			h.observe(ctx, Observation{Command: "pipeline", Count: len(cmds), Duration: time.Since(started), Err: err})
			return err
		}
		ctx, span := h.config.Application.Start(ctx, "redis", "storage.redis.pipeline",
			attribute.String("db.system.name", "redis"),
			attribute.String("db.operation.name", "pipeline"),
			attribute.Int("db.redis.command_count", len(cmds)),
		)
		err := next(ctx, cmds)
		gotel.End(span, err)
		h.observe(ctx, Observation{Command: "pipeline", Count: len(cmds), Duration: time.Since(started), Err: err})
		return err
	}
}

func (h *hook) observe(ctx context.Context, observation Observation) {
	if h.config.Observe != nil {
		h.config.Observe(ctx, observation)
	}
}
