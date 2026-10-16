// Package logtest provides an in-memory recorder for asserting on log output
// in unit tests.
//
//	rec := logtest.NewRecorder()
//	log := logger.MustNew(logger.WithHandler(rec), logger.WithLevel(logger.LevelDebug))
//
//	svc := NewService(log)
//	svc.Do()
//
//	if !rec.Has(logger.LevelWarn, "retrying") { t.Fatal("expected retry warning") }
//	e := rec.Last()
//	if e.Attrs["attempt"] != int64(2) { ... }
package logtest

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/techpartners-asia/logger-go"
)

// Entry is one recorded log line with attributes flattened into a map.
// Group attributes are nested maps; WithGroup prefixes keys with "group.".
type Entry struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

// Get returns a (possibly nested, dot-separated) attribute value.
func (e Entry) Get(path string) (any, bool) {
	cur := any(e.Attrs)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// String returns the attribute as a string, or "" when absent.
func (e Entry) String(path string) string {
	v, ok := e.Get(path)
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return slog.AnyValue(v).String()
}

// Recorder is a slog.Handler that stores entries in memory. It is safe for
// concurrent use. Handlers derived with WithAttrs/WithGroup record into the
// same Recorder.
type Recorder struct {
	store *store
	level slog.Level

	pre    []preAttr // attrs from WithAttrs, each with the group path at that time
	groups []string
}

type store struct {
	mu      sync.Mutex
	entries []Entry
}

type preAttr struct {
	groups []string
	attr   slog.Attr
}

// NewRecorder creates a recorder that captures every level.
func NewRecorder() *Recorder {
	return &Recorder{store: &store{}, level: logger.LevelTrace}
}

// New returns a logger that writes only to a new Recorder.
func New(opts ...logger.Option) (*logger.Logger, *Recorder) {
	rec := NewRecorder()
	all := append([]logger.Option{logger.WithHandler(rec), logger.WithLevel(logger.LevelTrace)}, opts...)
	return logger.MustNew(all...), rec
}

func (r *Recorder) Enabled(_ context.Context, l slog.Level) bool { return l >= r.level }

func (r *Recorder) Handle(_ context.Context, rec slog.Record) error {
	attrs := make(map[string]any)
	for _, p := range r.pre {
		putAttr(attrs, p.groups, p.attr)
	}
	rec.Attrs(func(a slog.Attr) bool {
		putAttr(attrs, r.groups, a)
		return true
	})
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.entries = append(r.store.entries, Entry{Time: rec.Time, Level: rec.Level, Message: rec.Message, Attrs: attrs})
	return nil
}

func (r *Recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *r
	c.pre = append([]preAttr(nil), r.pre...)
	for _, a := range attrs {
		c.pre = append(c.pre, preAttr{groups: r.groups, attr: a})
	}
	return &c
}

func (r *Recorder) WithGroup(name string) slog.Handler {
	if name == "" {
		return r
	}
	c := *r
	c.groups = append(append([]string(nil), r.groups...), name)
	return &c
}

func putAttr(m map[string]any, groups []string, a slog.Attr) {
	v := a.Value.Resolve()
	target := m
	for _, g := range groups {
		sub, ok := target[g].(map[string]any)
		if !ok {
			sub = make(map[string]any)
			target[g] = sub
		}
		target = sub
	}
	if v.Kind() == slog.KindGroup {
		if a.Key == "" {
			for _, ga := range v.Group() {
				putAttr(target, nil, ga)
			}
			return
		}
		sub := make(map[string]any)
		for _, ga := range v.Group() {
			putAttr(sub, nil, ga)
		}
		target[a.Key] = sub
		return
	}
	if a.Key == "" {
		return
	}
	target[a.Key] = v.Any()
}

// Entries returns a copy of all recorded entries.
func (r *Recorder) Entries() []Entry {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	return append([]Entry(nil), r.store.entries...)
}

// Len returns the number of entries.
func (r *Recorder) Len() int {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	return len(r.store.entries)
}

// Last returns the most recent entry, or a zero Entry when empty.
func (r *Recorder) Last() Entry {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if len(r.store.entries) == 0 {
		return Entry{}
	}
	return r.store.entries[len(r.store.entries)-1]
}

// Reset discards recorded entries.
func (r *Recorder) Reset() {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.entries = nil
}

// Has reports whether any entry at level has msgSubstring in its message.
func (r *Recorder) Has(level slog.Level, msgSubstring string) bool {
	return len(r.Find(level, msgSubstring)) > 0
}

// Find returns entries at level whose message contains msgSubstring.
func (r *Recorder) Find(level slog.Level, msgSubstring string) []Entry {
	var out []Entry
	for _, e := range r.Entries() {
		if e.Level == level && strings.Contains(e.Message, msgSubstring) {
			out = append(out, e)
		}
	}
	return out
}

// Messages returns all messages in order.
func (r *Recorder) Messages() []string {
	es := r.Entries()
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Message
	}
	return out
}
