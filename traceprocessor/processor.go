// Package traceprocessor provides whole-trace queueing and background-operation retention.
package traceprocessor

import (
	"context"
	"sync"
	"time"

	"github.com/zfogg/gotel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	Application                         gotel.Application
	Exporter                            sdktrace.SpanExporter
	ExportTimeout                       time.Duration
	QueueLimit, SpanLimit, HealthyEvery int
}
type operationKey struct{}
type operationState struct {
	mu                                 sync.Mutex
	name                               string
	root                               trace.SpanID
	keep, failed, important, streaming bool
	total, pending                     int
	spans                              []sdktrace.ReadOnlySpan
	ended                              sdktrace.ReadOnlySpan
}

type Operations struct{ application gotel.Application }

func NewOperations(application gotel.Application) Operations {
	return Operations{application: application}
}
func (o Operations) Start(ctx context.Context, name string) (context.Context, func(error)) {
	if trace.SpanContextFromContext(ctx).IsValid() {
		ctx, span := o.application.ComponentTracer(gotel.ComponentOperations).Start(ctx, name)
		return ctx, func(err error) { gotel.End(span, err) }
	}
	state := &operationState{name: name}
	ctx = context.WithValue(ctx, operationKey{}, state)
	ctx, span := o.application.ComponentTracer(gotel.ComponentBackground).Start(ctx, name)
	return ctx, func(err error) {
		state.mu.Lock()
		total, failed := state.total, state.failed
		state.mu.Unlock()
		span.SetAttributes(attribute.Int("gotel.telemetry.spans.total", total+1), attribute.Bool("spaniel.trace.partial", false))
		if failed {
			span.SetStatus(codes.Error, "background operation contains failed spans")
		}
		gotel.End(span, err)
	}
}
func Retain(ctx context.Context) {
	if state, ok := ctx.Value(operationKey{}).(*operationState); ok {
		state.mu.Lock()
		state.important = true
		state.mu.Unlock()
	}
}

func New(config Config) sdktrace.SpanProcessor {
	if config.ExportTimeout <= 0 {
		config.ExportTimeout = 10 * time.Second
	}
	if config.QueueLimit <= 0 {
		config.QueueLimit = 8192
	}
	if config.SpanLimit <= 0 {
		config.SpanLimit = 8192
	}
	if config.HealthyEvery <= 0 {
		config.HealthyEvery = 20
	}
	return newRetentionProcessor(config, newWholeTraceProcessor(config))
}

type traceBatch struct {
	spans []sdktrace.ReadOnlySpan
	done  chan error
	stop  bool
}
type pendingTrace struct {
	pending, total int
	spans          []sdktrace.ReadOnlySpan
	oversized      bool
}
type wholeTraceProcessor struct {
	control, mu                   sync.Mutex
	active                        map[trace.TraceID]*pendingTrace
	queue                         chan traceBatch
	queued, queueLimit, spanLimit int
	stopped                       bool
	exporter                      sdktrace.SpanExporter
	timeout                       time.Duration
	dropped, droppedSpans         metric.Int64Counter
}

func newWholeTraceProcessor(c Config) *wholeTraceProcessor {
	meter := otel.Meter(c.Application.Prefix() + "/telemetry")
	d, _ := meter.Int64Counter(c.Application.Prefix() + ".telemetry.dropped_traces")
	ds, _ := meter.Int64Counter(c.Application.Prefix() + ".telemetry.dropped_spans")
	p := &wholeTraceProcessor{active: map[trace.TraceID]*pendingTrace{}, queue: make(chan traceBatch, c.QueueLimit), queueLimit: c.QueueLimit, spanLimit: c.SpanLimit, exporter: c.Exporter, timeout: c.ExportTimeout, dropped: d, droppedSpans: ds}
	go p.run()
	return p
}
func (p *wholeTraceProcessor) OnStart(_ context.Context, s sdktrace.ReadWriteSpan) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	id := s.SpanContext().TraceID()
	st := p.active[id]
	if st == nil {
		st = &pendingTrace{}
		p.active[id] = st
	}
	st.pending++
}
func (p *wholeTraceProcessor) discardTrace(id trace.TraceID) {
	p.mu.Lock()
	delete(p.active, id)
	p.mu.Unlock()
}
func (p *wholeTraceProcessor) drop(total int, reason string) {
	o := metric.WithAttributes(attribute.String("reason", reason))
	p.dropped.Add(context.Background(), 1, o)
	p.droppedSpans.Add(context.Background(), int64(total), o)
}
func (p *wholeTraceProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.mu.Lock()
	id := s.SpanContext().TraceID()
	st := p.active[id]
	if st == nil {
		p.mu.Unlock()
		return
	}
	st.pending--
	st.total++
	if !st.oversized {
		if len(st.spans) >= p.spanLimit {
			st.oversized = true
			st.spans = nil
		} else {
			st.spans = append(st.spans, s)
		}
	}
	if st.pending > 0 {
		p.mu.Unlock()
		return
	}
	delete(p.active, id)
	reason := ""
	switch {
	case st.oversized:
		reason = "trace_limit"
	case p.stopped:
		reason = "shutdown"
	case p.queued+len(st.spans) > p.queueLimit:
		reason = "queue_full"
	default:
		select {
		case p.queue <- traceBatch{spans: st.spans}:
			p.queued += len(st.spans)
		default:
			reason = "queue_full"
		}
	}
	p.mu.Unlock()
	if reason != "" {
		p.drop(st.total, reason)
	}
}
func (p *wholeTraceProcessor) run() {
	for item := range p.queue {
		if len(item.spans) > 0 {
			p.mu.Lock()
			p.queued -= len(item.spans)
			p.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
			err := p.exporter.ExportSpans(ctx, item.spans)
			cancel()
			if err != nil {
				p.drop(len(item.spans), "export_error")
				otel.Handle(err)
			}
		}
		if item.stop {
			ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
			err := p.exporter.Shutdown(ctx)
			cancel()
			item.done <- err
			return
		}
		if item.done != nil {
			item.done <- nil
		}
	}
}
func (p *wholeTraceProcessor) barrier(ctx context.Context, stop bool) error {
	item := traceBatch{done: make(chan error, 1), stop: stop}
	select {
	case p.queue <- item:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-item.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *wholeTraceProcessor) ForceFlush(ctx context.Context) error {
	p.control.Lock()
	defer p.control.Unlock()
	p.mu.Lock()
	stopped := p.stopped
	p.mu.Unlock()
	if stopped {
		return nil
	}
	return p.barrier(ctx, false)
}
func (p *wholeTraceProcessor) Shutdown(ctx context.Context) error {
	p.control.Lock()
	defer p.control.Unlock()
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	p.stopped = true
	p.mu.Unlock()
	return p.barrier(ctx, true)
}

type retentionProcessor struct {
	next   sdktrace.SpanProcessor
	every  int
	mu     sync.Mutex
	active map[trace.TraceID]*operationState
	counts map[string]uint64
}

func newRetentionProcessor(c Config, next sdktrace.SpanProcessor) *retentionProcessor {
	return &retentionProcessor{next: next, every: c.HealthyEvery, active: map[trace.TraceID]*operationState{}, counts: map[string]uint64{}}
}
func (p *retentionProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	p.mu.Lock()
	state, _ := ctx.Value(operationKey{}).(*operationState)
	if state != nil && !s.Parent().IsValid() {
		state.root = s.SpanContext().SpanID()
		state.keep = p.counts[state.name]%uint64(p.every) == 0
		p.counts[state.name]++
		p.active[s.SpanContext().TraceID()] = state
	}
	if active := p.active[s.SpanContext().TraceID()]; active != nil {
		active.mu.Lock()
		active.pending++
		active.mu.Unlock()
	}
	p.mu.Unlock()
	p.next.OnStart(ctx, s)
}
func (p *retentionProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.mu.Lock()
	state := p.active[s.SpanContext().TraceID()]
	if state == nil {
		p.mu.Unlock()
		p.next.OnEnd(s)
		return
	}
	state.mu.Lock()
	state.pending--
	if s.Status().Code == codes.Error {
		state.failed = true
	}
	if state.root != s.SpanContext().SpanID() {
		state.total++
		state.spans = append(state.spans, s)
	} else {
		state.ended = s
	}
	complete := state.ended != nil && state.pending == 0
	keep := state.keep || state.failed || state.important
	if complete {
		delete(p.active, s.SpanContext().TraceID())
	}
	buffer := state.spans
	root := state.ended
	state.mu.Unlock()
	p.mu.Unlock()
	if complete && !keep {
		if sink, ok := p.next.(interface{ discardTrace(trace.TraceID) }); ok {
			sink.discardTrace(s.SpanContext().TraceID())
		}
	}
	if complete && keep {
		p.next.OnEnd(root)
		for _, child := range buffer {
			p.next.OnEnd(child)
		}
	}
}
func (p *retentionProcessor) Shutdown(ctx context.Context) error   { return p.next.Shutdown(ctx) }
func (p *retentionProcessor) ForceFlush(ctx context.Context) error { return p.next.ForceFlush(ctx) }
