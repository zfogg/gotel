// Package sampling provides application-configurable critical and realtime head
// decisions, plus bounded linked observations for failures discovered afterward.
package sampling

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/zfogg/gotel/go/outcome"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Matcher extracts application-owned naming/classes from a sampler. Empty lists
// match nothing; libraries never embed trading/media business names.
type Matcher struct {
	// NameContains matches case-insensitive nonempty name fragments.
	NameContains []string
	// TemporalFamilies matches workflow/activity type fragments after SDK or Gotel operation prefixes.
	TemporalFamilies []string
	// Classes matches operation.class or telemetry.operation.class at span start.
	Classes []string
}

// Match evaluates configured names and classes without retaining input/state.
func (m Matcher) Match(p sdktrace.SamplingParameters) bool {
	name := strings.ToLower(p.Name)
	for _, part := range m.NameContains {
		if part != "" && strings.Contains(name, strings.ToLower(part)) {
			return true
		}
	}
	for _, boundary := range []string{"startworkflow:", "runworkflow:", "startchildworkflow:", "runchildworkflow:", "startactivity:", "runactivity:", "temporal.workflow.", "temporal.activity.schedule.", "temporal.activity.execute."} {
		if index := strings.LastIndex(name, boundary); index >= 0 {
			for _, family := range m.TemporalFamilies {
				if family != "" && strings.Contains(name[index+len(boundary):], strings.ToLower(family)) {
					return true
				}
			}
		}
	}
	for _, attr := range p.Attributes {
		if attr.Key == "operation.class" || attr.Key == "telemetry.operation.class" {
			for _, class := range m.Classes {
				if strings.EqualFold(attr.Value.AsString(), class) {
					return true
				}
			}
		}
	}
	return false
}

// Config is immutable after New. Predicates and Now must be safe for concurrent use.
type Config struct {
	// Delegate handles ordinary operations; nil uses ParentBased(AlwaysSample()).
	Delegate sdktrace.Sampler
	// Critical selects operations that must be head-sampled; nil matches nothing.
	Critical func(sdktrace.SamplingParameters) bool
	// Realtime selects rate/cap-controlled operations; nil disables that path.
	Realtime func(sdktrace.SamplingParameters) bool
	// RealtimeRate is a finite sampling fraction in [0,1], default zero.
	RealtimeRate float64
	// RealtimeMaxPerSecond bounds selected independent roots; must be positive when Realtime is set.
	RealtimeMaxPerSecond uint32
	// Now is injectable for deterministic tests. It must be safe for concurrent use.
	Now func() time.Time
}

// Policy implements SDK head sampling. Share one instance to share rate limits.
type Policy struct {
	config Config
	limit  limiter
	ratio  sdktrace.Sampler
}

// New validates explicit rates/caps. Configure this sampler on the provider used
// by Retainer. Attributes added after Start cannot affect a head decision.
func New(config Config) (*Policy, error) {
	if math.IsNaN(config.RealtimeRate) || math.IsInf(config.RealtimeRate, 0) || config.RealtimeRate < 0 || config.RealtimeRate > 1 {
		return nil, fmt.Errorf("realtime rate must be finite and in [0,1]")
	}
	if config.Realtime != nil && config.RealtimeMaxPerSecond == 0 {
		return nil, fmt.Errorf("realtime cap must be positive")
	}
	if config.Delegate == nil {
		config.Delegate = sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Policy{config: config, ratio: sdktrace.TraceIDRatioBased(config.RealtimeRate)}, nil
}

// ShouldSample applies retained/critical, realtime, then ordinary delegate policy.
func (p *Policy) ShouldSample(params sdktrace.SamplingParameters) sdktrace.SamplingResult {
	retained := false
	for _, attr := range params.Attributes {
		if attr.Key == "gotel.observation.retained" && attr.Value.AsBool() {
			retained = true
		}
	}
	if retained || p.config.Critical != nil && p.config.Critical(params) {
		return sdktrace.AlwaysSample().ShouldSample(params)
	}
	if p.config.Realtime != nil && p.config.Realtime(params) {
		parent := trace.SpanContextFromContext(params.ParentContext)
		if parent.IsValid() {
			if parent.IsSampled() {
				return sdktrace.AlwaysSample().ShouldSample(params)
			}
			return sdktrace.NeverSample().ShouldSample(params)
		}
		result := p.ratio.ShouldSample(params)
		if result.Decision == sdktrace.RecordAndSample && !p.limit.allow(p.config.Now(), p.config.RealtimeMaxPerSecond) {
			return sdktrace.NeverSample().ShouldSample(params)
		}
		return result
	}
	return p.config.Delegate.ShouldSample(params)
}

func (p *Policy) Description() string { return "gotel.policy(" + p.config.Delegate.Description() + ")" }

type limiter struct {
	sync.Mutex
	window time.Time
	count  uint32
}

func (l *limiter) allow(now time.Time, cap uint32) bool {
	l.Lock()
	defer l.Unlock()
	if l.window.IsZero() || now.Sub(l.window) >= time.Second {
		l.window = now
		l.count = 0
	}
	if l.count >= cap {
		return false
	}
	l.count++
	return true
}

// Retainer emits independent linked observations. It never mutates the original
// dropped span. MaxPerSecond must be positive; zero disables retention.
// Use a provider configured with Policy so the retention marker is sampled.
type Retainer struct {
	// Tracer must use a provider with Policy; the caller owns provider lifetime.
	Tracer trace.Tracer
	// MaxPerSecond bounds independently retained errors; zero disables retention.
	MaxPerSecond uint32
	// Now defaults to time.Now and must be safe for concurrent use.
	Now   func() time.Time
	limit limiter
}

// Observe retains only Error outcomes whose original span is not sampled. The
// original span's attributes are unavailable; provide only safe explicit attrs.
// duration describes the completed operation; no unbounded buffering is used.
func (r *Retainer) Observe(ctx context.Context, name string, result outcome.Value, duration time.Duration, attrs ...attribute.KeyValue) bool {
	parent := trace.SpanContextFromContext(ctx)
	if r == nil || r.Tracer == nil || r.MaxPerSecond == 0 || result != outcome.Error || parent.IsSampled() {
		return false
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	if !r.limit.allow(now, r.MaxPerSecond) {
		return false
	}
	if duration < 0 {
		duration = 0
	}
	options := []trace.SpanStartOption{trace.WithNewRoot(), trace.WithTimestamp(now.Add(-duration)), trace.WithAttributes(attrs...), trace.WithAttributes(attribute.Bool("gotel.observation.retained", true))}
	if parent.IsValid() {
		options = append(options, trace.WithLinks(trace.Link{SpanContext: parent}))
	}
	_, span := r.Tracer.Start(ctx, name, options...)
	outcome.Apply(span, result, nil)
	span.End(trace.WithTimestamp(now))
	return span.IsRecording() || span.SpanContext().IsSampled()
}
