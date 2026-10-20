package gotel

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// FanoutHandler writes each record to every enabled handler. When a valid span
// is present, it adds trace_id and span_id for log/trace correlation.
type FanoutHandler struct {
	handlers []slog.Handler
}

// NewFanoutHandler combines handlers into one correlated slog handler.
func NewFanoutHandler(handlers ...slog.Handler) *FanoutHandler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, handler := range handlers {
		if handler != nil {
			filtered = append(filtered, handler)
		}
	}
	return &FanoutHandler{handlers: filtered}
}

func (h *FanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *FanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	var errs []error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			errs = append(errs, handler.Handle(ctx, record.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (h *FanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &FanoutHandler{handlers: handlers}
}

func (h *FanoutHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &FanoutHandler{handlers: handlers}
}

// RouteStandardLog sends package log output to logger at Info level.
func RouteStandardLog(logger *slog.Logger) {
	log.SetFlags(0)
	log.SetOutput(slogWriter{logger: logger})
}

type slogWriter struct{ logger *slog.Logger }

func (w slogWriter) Write(p []byte) (int, error) {
	w.logger.Info(strings.TrimRight(string(p), "\r\n"))
	return len(p), nil
}

var _ io.Writer = slogWriter{}
