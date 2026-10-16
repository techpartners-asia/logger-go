package logger

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Sampling limits how many records with the same level and message are
// emitted per Tick. The first Initial records in a tick are always logged;
// after that only every Thereafter-th record is. Records at or above
// ExemptLevel (default ERROR) are never dropped.
//
// Sampling{Initial: 100, Thereafter: 100, Tick: time.Second} is a good
// production default: it logs the first 100 occurrences of a message each
// second and then 1 in 100.
type Sampling struct {
	Initial     int
	Thereafter  int
	Tick        time.Duration
	ExemptLevel *Level
	// OnDropped is called with the number of records dropped for a message
	// at the end of each tick window. Optional; useful for a metric.
	OnDropped func(level Level, msg string, dropped uint64)
}

func (s Sampling) withDefaults() Sampling {
	if s.Initial <= 0 {
		s.Initial = 100
	}
	if s.Thereafter <= 0 {
		s.Thereafter = 100
	}
	if s.Tick <= 0 {
		s.Tick = time.Second
	}
	if s.ExemptLevel == nil {
		l := LevelError
		s.ExemptLevel = &l
	}
	return s
}

// SamplingHandler implements Sampling.
type SamplingHandler struct {
	next     slog.Handler
	cfg      Sampling
	counters sync.Map // sampleKey -> *counter
}

type sampleKey struct {
	level slog.Level
	msg   string
}

type counter struct {
	resetAt atomic.Int64 // unix nanos of the current window end
	n       atomic.Uint64
	dropped atomic.Uint64
}

// NewSamplingHandler wraps next.
func NewSamplingHandler(next slog.Handler, cfg Sampling) *SamplingHandler {
	return &SamplingHandler{next: next, cfg: cfg.withDefaults()}
}

func (h *SamplingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *SamplingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= *h.cfg.ExemptLevel {
		return h.next.Handle(ctx, r)
	}
	if !h.allow(r.Level, r.Message, r.Time) {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *SamplingHandler) allow(level slog.Level, msg string, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	key := sampleKey{level: level, msg: msg}
	v, _ := h.counters.LoadOrStore(key, &counter{})
	c := v.(*counter)

	nowNs := now.UnixNano()
	for {
		end := c.resetAt.Load()
		if nowNs < end {
			break
		}
		newEnd := nowNs + int64(h.cfg.Tick)
		if c.resetAt.CompareAndSwap(end, newEnd) {
			if dropped := c.dropped.Swap(0); dropped > 0 && h.cfg.OnDropped != nil {
				h.cfg.OnDropped(level, msg, dropped)
			}
			c.n.Store(0)
			break
		}
	}

	n := c.n.Add(1)
	if n <= uint64(h.cfg.Initial) {
		return true
	}
	if (n-uint64(h.cfg.Initial))%uint64(h.cfg.Thereafter) == 0 {
		return true
	}
	c.dropped.Add(1)
	return false
}

func (h *SamplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Counters are intentionally shared across derived handlers so sampling is
	// per message, not per With() chain.
	return &samplingChild{SamplingHandler: h, next: h.next.WithAttrs(attrs)}
}

func (h *SamplingHandler) WithGroup(name string) slog.Handler {
	return &samplingChild{SamplingHandler: h, next: h.next.WithGroup(name)}
}

type samplingChild struct {
	*SamplingHandler
	next slog.Handler
}

func (c *samplingChild) Enabled(ctx context.Context, l slog.Level) bool {
	return c.next.Enabled(ctx, l)
}

func (c *samplingChild) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= *c.cfg.ExemptLevel {
		return c.next.Handle(ctx, r)
	}
	if !c.allow(r.Level, r.Message, r.Time) {
		return nil
	}
	return c.next.Handle(ctx, r)
}

func (c *samplingChild) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &samplingChild{SamplingHandler: c.SamplingHandler, next: c.next.WithAttrs(attrs)}
}

func (c *samplingChild) WithGroup(name string) slog.Handler {
	return &samplingChild{SamplingHandler: c.SamplingHandler, next: c.next.WithGroup(name)}
}
