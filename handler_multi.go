package logger

import (
	"context"
	"errors"
	"log/slog"
)

// MultiHandler fans records out to several handlers, for example JSON to a
// file and console to stderr. A record is delivered to every handler that
// reports Enabled for its level.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler creates a fan-out handler. nil entries are ignored.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	hs := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			hs = append(hs, h)
		}
	}
	return &MultiHandler{handlers: hs}
}

func (m *MultiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: hs}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: hs}
}

// LevelHandler restricts a handler to records at or above a level. Combine
// with MultiHandler to send only WARN+ to a second destination.
type LevelHandler struct {
	slog.Handler
	level slog.Leveler
}

// NewLevelHandler wraps next with a minimum level.
func NewLevelHandler(level slog.Leveler, next slog.Handler) *LevelHandler {
	return &LevelHandler{Handler: next, level: level}
}

func (h *LevelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level.Level() && h.Handler.Enabled(ctx, l)
}

func (h *LevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LevelHandler{Handler: h.Handler.WithAttrs(attrs), level: h.level}
}

func (h *LevelHandler) WithGroup(name string) slog.Handler {
	return &LevelHandler{Handler: h.Handler.WithGroup(name), level: h.level}
}
