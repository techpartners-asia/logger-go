package logger

import (
	"context"
	"log/slog"
	"strings"
)

// DefaultRedactedKeys are masked when redaction is enabled. Matching is
// case-insensitive and ignores "-", "_" and ".", so "X-Api-Key", "api_key"
// and "apiKey" all match "apikey".
var DefaultRedactedKeys = []string{
	"password", "passwd", "pwd", "secret", "token", "access_token",
	"refresh_token", "id_token", "authorization", "auth", "api_key", "apikey",
	"x_api_key", "client_secret", "private_key", "credit_card", "card_number",
	"card", "pan", "cvv", "cvc", "pin", "ssn", "cookie", "set_cookie",
	"session", "session_id", "otp", "bearer", "credentials", "signature",
}

// DefaultRedactReplacement is the value written in place of a redacted one.
const DefaultRedactReplacement = "[REDACTED]"

// Redaction configures the RedactHandler.
type Redaction struct {
	// Keys to mask, in addition to DefaultRedactedKeys unless
	// DisableDefaults is set.
	Keys []string
	// DisableDefaults uses only Keys.
	DisableDefaults bool
	// Replacement overrides DefaultRedactReplacement.
	Replacement string
	// KeepLast, when > 0, keeps the last N characters of string values
	// visible: "****1234". Non-string values are fully replaced.
	KeepLast int
	// Match, when set, decides per normalised key whether to redact, replacing
	// the key-set lookup entirely.
	Match func(normalizedKey string) bool
}

// RedactHandler masks sensitive values before they reach the output handler.
// It inspects record attributes, attributes added with WithAttrs, and nested
// groups.
type RedactHandler struct {
	next slog.Handler
	keys map[string]struct{}
	cfg  Redaction
}

// NewRedactHandler wraps next.
func NewRedactHandler(next slog.Handler, cfg Redaction) *RedactHandler {
	keys := make(map[string]struct{}, len(DefaultRedactedKeys)+len(cfg.Keys))
	if !cfg.DisableDefaults {
		for _, k := range DefaultRedactedKeys {
			keys[normalizeKey(k)] = struct{}{}
		}
	}
	for _, k := range cfg.Keys {
		keys[normalizeKey(k)] = struct{}{}
	}
	if cfg.Replacement == "" {
		cfg.Replacement = DefaultRedactReplacement
	}
	return &RedactHandler{next: next, keys: keys, cfg: cfg}
}

func normalizeKey(k string) string {
	var b strings.Builder
	b.Grow(len(k))
	for _, r := range strings.ToLower(k) {
		if r == '-' || r == '_' || r == '.' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ShouldRedact reports whether a key would be masked.
func (h *RedactHandler) ShouldRedact(key string) bool {
	nk := normalizeKey(key)
	if h.cfg.Match != nil {
		return h.cfg.Match(nk)
	}
	_, ok := h.keys[nk]
	return ok
}

func (h *RedactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.NumAttrs() == 0 {
		return h.next.Handle(ctx, r)
	}
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, nr)
}

func (h *RedactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = h.redactAttr(a)
	}
	return &RedactHandler{next: h.next.WithAttrs(out), keys: h.keys, cfg: h.cfg}
}

func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{next: h.next.WithGroup(name), keys: h.keys, cfg: h.cfg}
}

func (h *RedactHandler) redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		gs := v.Group()
		out := make([]slog.Attr, len(gs))
		for i, ga := range gs {
			out[i] = h.redactAttr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	if !h.ShouldRedact(a.Key) {
		return a
	}
	return slog.Attr{Key: a.Key, Value: h.mask(v)}
}

func (h *RedactHandler) mask(v slog.Value) slog.Value {
	if h.cfg.KeepLast > 0 && v.Kind() == slog.KindString {
		s := v.String()
		if len(s) > h.cfg.KeepLast {
			return slog.StringValue(strings.Repeat("*", 4) + s[len(s)-h.cfg.KeepLast:])
		}
	}
	return slog.StringValue(h.cfg.Replacement)
}
