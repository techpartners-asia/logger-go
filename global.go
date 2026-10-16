package logger

import (
	"context"
	"log/slog"
	"sync/atomic"
)

var defaultLogger atomic.Pointer[Logger]

func init() {
	defaultLogger.Store(MustNew())
}

// Default returns the process-wide logger. Until SetDefault is called it is a
// JSON logger at INFO writing to stderr.
func Default() *Logger { return defaultLogger.Load() }

// SetDefault replaces the process-wide logger and also installs it as the
// log/slog default so slog.Info(...) and third-party slog users share the
// same output. Pass redirectStd=true in RedirectStdLog to also capture the
// standard "log" package.
func SetDefault(l *Logger) {
	if l == nil {
		return
	}
	defaultLogger.Store(l)
	slog.SetDefault(l.Slog())
}

// Package-level shortcuts that delegate to Default(). Source locations are
// still reported correctly.

func Trace(msg string, args ...any) { Default().log(nil, LevelTrace, msg, args) }
func Debug(msg string, args ...any) { Default().log(nil, LevelDebug, msg, args) }
func Info(msg string, args ...any)  { Default().log(nil, LevelInfo, msg, args) }
func Warn(msg string, args ...any)  { Default().log(nil, LevelWarn, msg, args) }
func Error(msg string, args ...any) { Default().log(nil, LevelError, msg, args) }
func Fatal(msg string, args ...any) {
	d := Default()
	d.log(nil, LevelFatal, msg, args)
	_ = d.Sync()
	d.shared.exit(1)
}

func TraceContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, LevelTrace, msg, args)
}
func DebugContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, LevelDebug, msg, args)
}
func InfoContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, LevelInfo, msg, args)
}
func WarnContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, LevelWarn, msg, args)
}
func ErrorContext(ctx context.Context, msg string, args ...any) {
	Default().log(ctx, LevelError, msg, args)
}

// With returns Default().With(args...).
func With(args ...any) *Logger { return Default().With(args...) }

// Named returns Default().Named(name).
func Named(name string) *Logger { return Default().Named(name) }

// SetLevel changes the default logger's level.
func SetLevel(l Level) { Default().SetLevel(l) }
