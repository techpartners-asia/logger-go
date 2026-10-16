package logger

import (
	"context"
	"io"
	"log/slog"
)

// Format selects the output encoding.
type Format string

const (
	// FormatJSON emits one JSON object per line. Use in production.
	FormatJSON Format = "json"
	// FormatConsole emits colored, human-readable lines. Use in development.
	FormatConsole Format = "console"
	// FormatText emits slog's key=value text format.
	FormatText Format = "text"
)

// ParseFormat converts a string ("json", "console"/"pretty"/"dev", "text") to a
// Format. Unknown values fall back to JSON.
func ParseFormat(s string) Format {
	switch s {
	case "console", "pretty", "dev", "human":
		return FormatConsole
	case "text", "logfmt":
		return FormatText
	default:
		return FormatJSON
	}
}

// Middleware wraps a slog.Handler with additional behaviour. Middlewares are
// applied in order, so the first one supplied is the outermost.
type Middleware func(slog.Handler) slog.Handler

// ContextExtractor pulls attributes out of a context. Extractors run on every
// record so they should be cheap and must never panic on a background context.
type ContextExtractor func(ctx context.Context) []Attr

// options holds everything New needs. It is only mutated by Option funcs.
type options struct {
	level       Level
	format      Format
	output      io.Writer
	closer      io.Closer
	file        *FileConfig
	color       *bool // nil = auto
	addSource   bool
	timeFormat  string
	utc         bool
	keys        KeyNames
	replaceAttr func(groups []string, a slog.Attr) slog.Attr

	service  string
	env      string
	version  string
	procInfo bool
	fields   []Attr

	redaction  *Redaction
	sampling   *Sampling
	stackLevel *Level
	extractors []ContextExtractor
	ctxKeys    []contextKeyMapping

	handler     slog.Handler
	middlewares []Middleware
	exit        func(int)
}

func defaultOptions() *options {
	return &options{
		level:  LevelInfo,
		format: FormatJSON,
		keys:   DefaultKeyNames,
	}
}

// Option configures a Logger.
type Option func(*options)

// WithLevel sets the minimum level. It can be changed later with SetLevel.
func WithLevel(l Level) Option { return func(o *options) { o.level = l } }

// WithLevelString sets the level from a name such as "debug". Invalid names are
// ignored and the level stays at its previous value.
func WithLevelString(s string) Option {
	return func(o *options) {
		if l, err := ParseLevel(s); err == nil {
			o.level = l
		}
	}
}

// WithFormat selects JSON, console or text output.
func WithFormat(f Format) Option { return func(o *options) { o.format = f } }

// WithOutput sets the destination writer. Default is os.Stderr. Writes are
// serialised by the handler, so the writer does not need its own locking.
func WithOutput(w io.Writer) Option { return func(o *options) { o.output = w } }

// WithOutputCloser sets a writer that Close should also close (for example a
// file or a rotating log writer).
func WithOutputCloser(w io.WriteCloser) Option {
	return func(o *options) { o.output = w; o.closer = w }
}

// WithColor forces color on or off for the console format. The default is to
// enable color only when the output is a terminal.
func WithColor(enabled bool) Option { return func(o *options) { o.color = &enabled } }

// WithSource adds the file:line of the log call to every record.
func WithSource(enabled bool) Option { return func(o *options) { o.addSource = enabled } }

// WithTimeFormat sets the time layout. Default is RFC3339 with milliseconds
// for JSON/text and "15:04:05.000" for console.
func WithTimeFormat(layout string) Option { return func(o *options) { o.timeFormat = layout } }

// WithUTC renders timestamps in UTC.
func WithUTC() Option { return func(o *options) { o.utc = true } }

// WithKeyNames renames the built-in time/level/message/source keys, for
// example to match a Datadog or Elastic schema.
func WithKeyNames(k KeyNames) Option { return func(o *options) { o.keys = k.withDefaults() } }

// WithReplaceAttr installs a custom slog ReplaceAttr function that runs after
// the built-in rewrites.
func WithReplaceAttr(fn func(groups []string, a slog.Attr) slog.Attr) Option {
	return func(o *options) { o.replaceAttr = fn }
}

// WithService sets the "service" field on every record.
func WithService(name string) Option { return func(o *options) { o.service = name } }

// WithEnv sets the "env" field on every record (production, staging, ...).
func WithEnv(env string) Option { return func(o *options) { o.env = env } }

// WithVersion sets the "version" field on every record.
func WithVersion(v string) Option { return func(o *options) { o.version = v } }

// WithProcessInfo adds "host" and "pid" fields to every record.
func WithProcessInfo() Option { return func(o *options) { o.procInfo = true } }

// WithFields adds static attributes to every record.
func WithFields(attrs ...Attr) Option {
	return func(o *options) { o.fields = append(o.fields, attrs...) }
}

// WithRedaction masks the values of sensitive keys. Passing no keys enables
// the DefaultRedactedKeys set; passing keys adds to that set.
func WithRedaction(extraKeys ...string) Option {
	return func(o *options) {
		if o.redaction == nil {
			o.redaction = &Redaction{}
		}
		o.redaction.Keys = append(o.redaction.Keys, extraKeys...)
	}
}

// WithRedactionConfig installs a fully custom Redaction.
func WithRedactionConfig(r Redaction) Option { return func(o *options) { o.redaction = &r } }

// WithSampling drops repeated messages beyond a threshold. See Sampling.
func WithSampling(s Sampling) Option { return func(o *options) { o.sampling = &s } }

// WithStackTrace attaches a "stack" attribute to records at or above level.
func WithStackTrace(level Level) Option { return func(o *options) { o.stackLevel = &level } }

// WithContextExtractor adds a function that derives attributes from the
// context passed to the *Context logging methods. Use it to pull trace IDs
// from your tracing library.
func WithContextExtractor(fn ContextExtractor) Option {
	return func(o *options) { o.extractors = append(o.extractors, fn) }
}

// WithContextValue logs the value stored in the context under ctxKey using
// the attribute name key, whenever it is present and non-nil.
func WithContextValue(key string, ctxKey any) Option {
	return func(o *options) {
		o.ctxKeys = append(o.ctxKeys, contextKeyMapping{attr: key, ctxKey: ctxKey})
	}
}

// WithHandler replaces the built-in base handler. Format, output, time and key
// options are ignored, but redaction, sampling, context fields and
// middlewares still apply on top of the supplied handler.
func WithHandler(h slog.Handler) Option { return func(o *options) { o.handler = h } }

// WithMiddleware wraps the handler chain with custom handler middleware. The
// first middleware supplied becomes the outermost.
func WithMiddleware(m ...Middleware) Option {
	return func(o *options) { o.middlewares = append(o.middlewares, m...) }
}

// WithExitFunc overrides the function called by Fatal (default os.Exit).
func WithExitFunc(fn func(code int)) Option { return func(o *options) { o.exit = fn } }

// WithConfig applies a Config. Options passed after it override its values.
func WithConfig(c Config) Option {
	return func(o *options) {
		for _, opt := range c.Options() {
			opt(o)
		}
	}
}

// KeyNames lets you rename the four built-in record keys.
type KeyNames struct {
	Time    string
	Level   string
	Message string
	Source  string
}

// DefaultKeyNames are slog's defaults: time, level, msg, source.
var DefaultKeyNames = KeyNames{
	Time:    slog.TimeKey,
	Level:   slog.LevelKey,
	Message: slog.MessageKey,
	Source:  slog.SourceKey,
}

// ElasticKeyNames follow the Elastic Common Schema.
var ElasticKeyNames = KeyNames{
	Time:    "@timestamp",
	Level:   "log.level",
	Message: "message",
	Source:  "log.origin",
}

// DatadogKeyNames match what Datadog's log pipeline expects by default.
var DatadogKeyNames = KeyNames{
	Time:    "timestamp",
	Level:   "status",
	Message: "message",
	Source:  "logger.source",
}

func (k KeyNames) withDefaults() KeyNames {
	if k.Time == "" {
		k.Time = slog.TimeKey
	}
	if k.Level == "" {
		k.Level = slog.LevelKey
	}
	if k.Message == "" {
		k.Message = slog.MessageKey
	}
	if k.Source == "" {
		k.Source = slog.SourceKey
	}
	return k
}

const (
	defaultTimeFormat        = "2006-01-02T15:04:05.000Z07:00"
	defaultConsoleTimeFormat = "15:04:05.000"
)

func (o *options) resolvedTimeFormat() string {
	if o.timeFormat != "" {
		return o.timeFormat
	}
	if o.format == FormatConsole {
		return defaultConsoleTimeFormat
	}
	return defaultTimeFormat
}
