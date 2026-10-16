package logger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// Logger is a leveled, structured logger. It is safe for concurrent use and
// cheap to copy via With/WithGroup/Named, which share the underlying handler
// and level.
//
// Logging methods accept slog-style arguments: alternating "key", value pairs
// and/or Attr values (logger.Err(err), logger.String("k", "v"), ...).
type Logger struct {
	h     slog.Handler
	level *slog.LevelVar
	name  string

	// shared between all derived loggers
	shared *shared
}

type shared struct {
	exit   func(int)
	closer io.Closer
	output io.Writer
	once   sync.Once
}

// New builds a Logger from the supplied options. It returns an error only
// when an output file cannot be opened.
func New(opts ...Option) (*Logger, error) {
	o := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	if ew, ok := o.output.(*errWriter); ok {
		return nil, fmt.Errorf("logger: open output: %w", ew.err)
	}
	if o.file != nil {
		fw, err := NewRotatingWriter(o.file.withDefaults(o.service))
		if err != nil {
			return nil, err
		}
		if o.file.Tee {
			other := o.output
			if other == nil {
				other = os.Stderr
			}
			o.output = &teeWriter{file: fw, other: other}
		} else {
			o.output = fw
		}
		o.closer = fw
	}
	if o.output == nil {
		o.output = os.Stderr
	}
	if o.exit == nil {
		o.exit = os.Exit
	}

	lv := new(slog.LevelVar)
	lv.Set(o.level)

	base := o.handler
	if base == nil {
		base = newBaseHandler(o, lv)
	}

	// Static fields go directly on the base handler so they are emitted first.
	if attrs := staticFields(o); len(attrs) > 0 {
		base = base.WithAttrs(attrs)
	}

	// Build the chain from the inside out. Outermost receives records first.
	h := base
	if o.redaction != nil {
		h = NewRedactHandler(h, *o.redaction)
	}
	if o.stackLevel != nil {
		h = NewStackHandler(h, *o.stackLevel)
	}
	if o.sampling != nil {
		h = NewSamplingHandler(h, *o.sampling)
	}
	h = newContextHandler(h, o.extractors, o.ctxKeys)
	for i := len(o.middlewares) - 1; i >= 0; i-- {
		if o.middlewares[i] != nil {
			h = o.middlewares[i](h)
		}
	}
	// A level guard on the outside makes Enabled cheap even for custom handlers.
	h = &levelVarHandler{Handler: h, level: lv}

	return &Logger{
		h:     h,
		level: lv,
		shared: &shared{
			exit:   o.exit,
			closer: o.closer,
			output: o.output,
		},
	}, nil
}

// MustNew is New but panics on error. Suitable for package initialisation.
func MustNew(opts ...Option) *Logger {
	l, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return l
}

// NewNop returns a logger that discards everything.
func NewNop() *Logger {
	return MustNew(WithOutput(io.Discard), WithLevel(LevelPanic+1))
}

// FromSlog wraps an existing *slog.Logger. Level switching is unavailable
// unless the handler respects the returned logger's SetLevel through its own
// means; everything else works.
func FromSlog(sl *slog.Logger) *Logger {
	if sl == nil {
		sl = slog.Default()
	}
	lv := new(slog.LevelVar)
	lv.Set(LevelTrace)
	return &Logger{
		h:      &levelVarHandler{Handler: sl.Handler(), level: lv},
		level:  lv,
		shared: &shared{exit: os.Exit, output: os.Stderr},
	}
}

func staticFields(o *options) []Attr {
	var attrs []Attr
	if o.service != "" {
		attrs = append(attrs, slog.String(KeyService, o.service))
	}
	if o.env != "" {
		attrs = append(attrs, slog.String(KeyEnv, o.env))
	}
	if o.version != "" {
		attrs = append(attrs, slog.String(KeyVersion, o.version))
	}
	if o.procInfo {
		if host, err := os.Hostname(); err == nil {
			attrs = append(attrs, slog.String(KeyHost, host))
		}
		attrs = append(attrs, slog.Int(KeyPID, os.Getpid()))
	}
	return append(attrs, o.fields...)
}

func newBaseHandler(o *options, lv *slog.LevelVar) slog.Handler {
	keys := o.keys.withDefaults()
	layout := o.resolvedTimeFormat()
	utc := o.utc
	user := o.replaceAttr

	replace := func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 {
			switch a.Key {
			case slog.TimeKey:
				if t, ok := a.Value.Any().(time.Time); ok {
					if utc {
						t = t.UTC()
					}
					a.Value = slog.StringValue(t.Format(layout))
				}
				a.Key = keys.Time
			case slog.LevelKey:
				a = replaceLevelName(groups, a)
				a.Key = keys.Level
			case slog.MessageKey:
				a.Key = keys.Message
			case slog.SourceKey:
				if src, ok := a.Value.Any().(*slog.Source); ok && src != nil {
					a.Value = slog.StringValue(shortSource(src))
				}
				a.Key = keys.Source
			}
		}
		if user != nil {
			a = user(groups, a)
		}
		return a
	}

	hopts := &slog.HandlerOptions{
		Level:       lv,
		AddSource:   o.addSource,
		ReplaceAttr: replace,
	}

	switch o.format {
	case FormatConsole:
		return NewConsoleHandler(o.output, &ConsoleOptions{
			Level:       lv,
			AddSource:   o.addSource,
			TimeFormat:  layout,
			UTC:         utc,
			Color:       o.color,
			ReplaceAttr: user,
		})
	case FormatText:
		return slog.NewTextHandler(o.output, hopts)
	default:
		return slog.NewJSONHandler(o.output, hopts)
	}
}

// shortSource renders "pkg/file.go:123" instead of the full absolute path.
func shortSource(s *slog.Source) string {
	file := s.File
	// keep the last two path elements
	slash := 0
	for i := len(file) - 1; i >= 0 && slash < 2; i-- {
		if file[i] == '/' {
			slash++
			if slash == 2 {
				file = file[i+1:]
			}
		}
	}
	return file + ":" + strconv.Itoa(s.Line)
}

// --- derived loggers -------------------------------------------------------

func (l *Logger) clone(h slog.Handler) *Logger {
	c := *l
	c.h = h
	return &c
}

// With returns a logger that includes the given attributes on every record.
func (l *Logger) With(args ...any) *Logger {
	attrs := argsToAttrs(args)
	if len(attrs) == 0 {
		return l
	}
	return l.clone(l.h.WithAttrs(attrs))
}

// WithAttrs is With for callers who already hold []Attr.
func (l *Logger) WithAttrs(attrs ...Attr) *Logger {
	if len(attrs) == 0 {
		return l
	}
	return l.clone(l.h.WithAttrs(attrs))
}

// WithGroup nests subsequent attributes under name.
func (l *Logger) WithGroup(name string) *Logger {
	if name == "" {
		return l
	}
	return l.clone(l.h.WithGroup(name))
}

// WithError returns a logger carrying the error under the "error" key.
func (l *Logger) WithError(err error) *Logger {
	if err == nil {
		return l
	}
	return l.WithAttrs(Err(err))
}

// Named returns a logger with a "logger" attribute. Nested calls join names
// with dots: Named("http").Named("client") yields "http.client".
func (l *Logger) Named(name string) *Logger {
	if name == "" {
		return l
	}
	full := name
	if l.name != "" {
		full = l.name + "." + name
	}
	c := l.clone(l.h.WithAttrs([]Attr{slog.String(KeyLogger, full)}))
	c.name = full
	return c
}

// --- levels ----------------------------------------------------------------

// Level returns the current minimum level.
func (l *Logger) Level() Level { return l.level.Level() }

// SetLevel changes the minimum level at runtime for this logger and every
// logger derived from the same New call.
func (l *Logger) SetLevel(level Level) { l.level.Set(level) }

// SetLevelString is SetLevel with a level name.
func (l *Logger) SetLevelString(s string) error {
	lv, err := ParseLevel(s)
	if err != nil {
		return err
	}
	l.level.Set(lv)
	return nil
}

// Enabled reports whether records at level would be emitted.
func (l *Logger) Enabled(ctx context.Context, level Level) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	return l.h.Enabled(ctx, level)
}

// --- interop ---------------------------------------------------------------

// Slog returns a *slog.Logger backed by this logger. Pass it to libraries that
// accept slog; records flow through the same handler chain.
func (l *Logger) Slog() *slog.Logger { return slog.New(l.h) }

// Handler returns the underlying slog.Handler.
func (l *Logger) Handler() slog.Handler { return l.h }

// --- emitting --------------------------------------------------------------

// Log emits a record at an arbitrary level.
func (l *Logger) Log(ctx context.Context, level Level, msg string, args ...any) {
	l.log(ctx, level, msg, args)
}

// LogAttrs is the allocation-free variant of Log.
func (l *Logger) LogAttrs(ctx context.Context, level Level, msg string, attrs ...Attr) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.h.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.AddAttrs(attrs...)
	_ = l.h.Handle(ctx, r)
}

func (l *Logger) Trace(msg string, args ...any) { l.log(nil, LevelTrace, msg, args) }
func (l *Logger) Debug(msg string, args ...any) { l.log(nil, LevelDebug, msg, args) }
func (l *Logger) Info(msg string, args ...any)  { l.log(nil, LevelInfo, msg, args) }
func (l *Logger) Warn(msg string, args ...any)  { l.log(nil, LevelWarn, msg, args) }
func (l *Logger) Error(msg string, args ...any) { l.log(nil, LevelError, msg, args) }

// Fatal logs at FATAL, flushes, and exits the process with status 1.
func (l *Logger) Fatal(msg string, args ...any) {
	l.log(nil, LevelFatal, msg, args)
	_ = l.Sync()
	l.shared.exit(1)
}

// Panic logs at PANIC, flushes, then panics with msg.
func (l *Logger) Panic(msg string, args ...any) {
	l.log(nil, LevelPanic, msg, args)
	_ = l.Sync()
	panic(msg)
}

// Context-aware variants. Context extractors and fields stored with
// ContextWithFields are only visible to these methods.

func (l *Logger) TraceContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelTrace, msg, args)
}
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelDebug, msg, args)
}
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelInfo, msg, args)
}
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelWarn, msg, args)
}
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelError, msg, args)
}
func (l *Logger) FatalContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, LevelFatal, msg, args)
	_ = l.Sync()
	l.shared.exit(1)
}

// Printf-style helpers for migrating code away from fmt/log.

func (l *Logger) Tracef(format string, a ...any) {
	l.log(nil, LevelTrace, fmt.Sprintf(format, a...), nil)
}
func (l *Logger) Debugf(format string, a ...any) {
	l.log(nil, LevelDebug, fmt.Sprintf(format, a...), nil)
}
func (l *Logger) Infof(format string, a ...any) {
	l.log(nil, LevelInfo, fmt.Sprintf(format, a...), nil)
}
func (l *Logger) Warnf(format string, a ...any) {
	l.log(nil, LevelWarn, fmt.Sprintf(format, a...), nil)
}
func (l *Logger) Errorf(format string, a ...any) {
	l.log(nil, LevelError, fmt.Sprintf(format, a...), nil)
}
func (l *Logger) Fatalf(format string, a ...any) {
	l.log(nil, LevelFatal, fmt.Sprintf(format, a...), nil)
	_ = l.Sync()
	l.shared.exit(1)
}

// log is the single emission path. Call depth: caller -> Info -> log, so the
// source PC is 3 frames up.
func (l *Logger) log(ctx context.Context, level Level, msg string, args []any) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.h.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = l.h.Handle(ctx, r)
}

// --- lifecycle -------------------------------------------------------------

// Sync flushes the output if it supports Sync (files do). Safe to call often.
func (l *Logger) Sync() error {
	type syncer interface{ Sync() error }
	if s, ok := l.shared.output.(syncer); ok {
		if err := s.Sync(); err != nil && !isIgnorableSyncErr(err) {
			return err
		}
	}
	return nil
}

// Close flushes and closes the output when it was opened by this package
// (WithOutputCloser or a file path from Config). It is idempotent.
func (l *Logger) Close() error {
	var err error
	l.shared.once.Do(func() {
		err = l.Sync()
		if l.shared.closer != nil {
			if cerr := l.shared.closer.Close(); cerr != nil {
				err = errors.Join(err, cerr)
			}
		}
	})
	return err
}

// isIgnorableSyncErr hides the EINVAL/ENOTTY noise from fsync on a terminal
// or pipe.
func isIgnorableSyncErr(err error) bool {
	var pe *os.PathError
	if errors.As(err, &pe) {
		s := pe.Err.Error()
		return s == "invalid argument" || s == "inappropriate ioctl for device" || s == "bad file descriptor"
	}
	return false
}

// levelVarHandler gates a handler chain on a shared LevelVar so SetLevel also
// works for custom handlers supplied with WithHandler or FromSlog.
type levelVarHandler struct {
	slog.Handler
	level *slog.LevelVar
}

func (h *levelVarHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level.Level() && h.Handler.Enabled(ctx, l)
}

func (h *levelVarHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelVarHandler{Handler: h.Handler.WithAttrs(attrs), level: h.level}
}

func (h *levelVarHandler) WithGroup(name string) slog.Handler {
	return &levelVarHandler{Handler: h.Handler.WithGroup(name), level: h.level}
}
