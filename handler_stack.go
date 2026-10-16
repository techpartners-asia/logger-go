package logger

import (
	"context"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
)

// StackHandler adds a "stack" attribute with a trimmed goroutine stack to
// records at or above a level. The frames belonging to this package and to
// log/slog are skipped so the trace starts at the call site.
type StackHandler struct {
	next  slog.Handler
	level slog.Level
}

// NewStackHandler wraps next.
func NewStackHandler(next slog.Handler, level slog.Level) *StackHandler {
	return &StackHandler{next: next, level: level}
}

func (h *StackHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *StackHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= h.level {
		hasStack := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == KeyStack {
				hasStack = true
				return false
			}
			return true
		})
		if !hasStack {
			r.AddAttrs(slog.String(KeyStack, captureStack(r.PC)))
		}
	}
	return h.next.Handle(ctx, r)
}

func (h *StackHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &StackHandler{next: h.next.WithAttrs(attrs), level: h.level}
}

func (h *StackHandler) WithGroup(name string) slog.Handler {
	return &StackHandler{next: h.next.WithGroup(name), level: h.level}
}

// captureStack returns a compact multi-line stack starting at the frame that
// matches pc (the log call site). Each line is "func\n\tfile:line".
func captureStack(pc uintptr) string {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(1, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	var b strings.Builder
	started := pc == 0
	for {
		f, more := frames.Next()
		if !started {
			// Skip until we reach the record's PC; fall back to skipping our
			// own package if the PC is not in this stack.
			if f.PC == pc || (pc != 0 && f.Entry == runtime.FuncForPC(pc).Entry()) {
				started = true
			} else if !more {
				break
			} else {
				continue
			}
		}
		if isInternalFrame(f.Function) {
			if !more {
				break
			}
			continue
		}
		b.WriteString(f.Function)
		b.WriteString("\n\t")
		b.WriteString(f.File)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(f.Line))
		if !more {
			break
		}
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		// pc not found in current stack: emit the whole thing minus internals.
		frames = runtime.CallersFrames(pcs[:n])
		for {
			f, more := frames.Next()
			if !isInternalFrame(f.Function) {
				b.WriteString(f.Function)
				b.WriteString("\n\t")
				b.WriteString(f.File)
				b.WriteByte(':')
				b.WriteString(strconv.Itoa(f.Line))
				b.WriteByte('\n')
			}
			if !more {
				break
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func isInternalFrame(fn string) bool {
	return strings.HasPrefix(fn, "log/slog.") ||
		strings.HasPrefix(fn, "runtime.") ||
		strings.Contains(fn, "techpartners-asia/logger-go.(*") ||
		strings.Contains(fn, "techpartners-asia/logger-go.log")
}

// runtimeFrames formats the PC's source as pkg/file.go:line for the console.
func runtimeFrames(pc uintptr) string {
	fs := runtime.CallersFrames([]uintptr{pc})
	f, _ := fs.Next()
	return shortSource(&slog.Source{File: f.File, Line: f.Line, Function: f.Function})
}
