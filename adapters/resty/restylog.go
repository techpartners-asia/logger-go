// Package restylog integrates the techpartners logger with Resty v3, the HTTP
// client used across techpartners SDKs.
//
//	client := resty.New()
//	restylog.Install(client, log)        // request log + resty's internal logger
//
//	resp, err := client.R().
//		SetContext(ctx).                 // request_id etc. flow through ctx
//		Get("https://api.example.com/x")
//
// Each call is logged once with method, url, status, duration_ms and, when
// present, the attempt count. Successful calls log at DEBUG by default so
// production stays quiet.
package restylog

import (
	"net/http"
	"strings"
	"time"

	"github.com/techpartners-asia/logger-go"
	"resty.dev/v3"
)

// RequestIDHeader is forwarded from the context's request ID when set.
const RequestIDHeader = "X-Request-ID"

// Config tunes the adapter.
type Config struct {
	// Level for successful calls. Default DEBUG.
	Level logger.Level
	// Message overrides the log text. Default "http client request".
	Message string
	// SlowThreshold logs slower calls at WARN.
	SlowThreshold time.Duration
	// LogRequestBody / LogResponseBody include bodies (truncated to BodyLimit).
	// Off by default; be careful with personal data.
	LogRequestBody  bool
	LogResponseBody bool
	BodyLimit       int // default 2048 bytes
	// DisablePropagation stops forwarding X-Request-ID from the context.
	DisablePropagation bool
	// Name becomes the "logger" attribute. Default "resty".
	Name string
}

func (c *Config) defaults() {
	if c.Level == 0 {
		c.Level = logger.LevelDebug
	}
	if c.Message == "" {
		c.Message = "http client request"
	}
	if c.BodyLimit == 0 {
		c.BodyLimit = 2048
	}
	if c.Name == "" {
		c.Name = "resty"
	}
}

// Install wires both the request/response middleware and resty's internal
// logger onto the client.
func Install(client *resty.Client, log *logger.Logger, cfgs ...Config) *resty.Client {
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	cfg.defaults()
	if log == nil {
		log = logger.Default()
	}
	l := log.Named(cfg.Name)
	client.SetLogger(NewLogger(l))
	client.AddRequestMiddleware(requestMiddleware(cfg))
	client.AddResponseMiddleware(responseMiddleware(l, cfg))
	client.OnError(errorHook(l, cfg))
	return client
}

type startKey struct{}

func requestMiddleware(cfg Config) resty.RequestMiddleware {
	return func(_ *resty.Client, r *resty.Request) error {
		r.SetContext(contextWithStart(r))
		if !cfg.DisablePropagation && r.Header.Get(RequestIDHeader) == "" {
			for _, a := range logger.FieldsFromContext(r.Context()) {
				if a.Key == logger.KeyRequestID {
					r.SetHeader(RequestIDHeader, a.Value.String())
					break
				}
			}
		}
		return nil
	}
}

func responseMiddleware(l *logger.Logger, cfg Config) resty.ResponseMiddleware {
	return func(_ *resty.Client, res *resty.Response) error {
		req := res.Request
		attrs := baseAttrs(req, res.Duration())
		attrs = append(attrs, logger.Int(logger.KeyStatus, res.StatusCode()), logger.Int64(logger.KeyBytes, res.Size()))
		if cfg.LogRequestBody {
			attrs = append(attrs, logger.String("request_body", truncate(bodyString(req.Body), cfg.BodyLimit)))
		}
		if cfg.LogResponseBody {
			attrs = append(attrs, logger.String("response_body", truncate(res.String(), cfg.BodyLimit)))
		}
		level := cfg.Level
		switch {
		case res.StatusCode() >= 500:
			level = logger.LevelError
		case res.StatusCode() >= 400:
			level = logger.LevelWarn
		case cfg.SlowThreshold > 0 && res.Duration() > cfg.SlowThreshold:
			level = logger.LevelWarn
			attrs = append(attrs, logger.Bool("slow", true))
		}
		l.LogAttrs(req.Context(), level, cfg.Message, attrs...)
		return nil
	}
}

func errorHook(l *logger.Logger, cfg Config) resty.ErrorHook {
	return func(req *resty.Request, err error) {
		var dur time.Duration
		if st, ok := req.Context().Value(startKey{}).(time.Time); ok {
			dur = time.Since(st)
		}
		attrs := append(baseAttrs(req, dur), logger.Err(err))
		l.LogAttrs(req.Context(), logger.LevelError, cfg.Message, attrs...)
	}
}

func baseAttrs(req *resty.Request, dur time.Duration) []logger.Attr {
	attrs := []logger.Attr{
		logger.String(logger.KeyMethod, req.Method),
		logger.String("url", req.URL),
		logger.Duration(dur),
	}
	if req.Attempt > 1 {
		attrs = append(attrs, logger.Int("attempt", req.Attempt))
	}
	return attrs
}

func bodyString(b any) string {
	switch v := b.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return "<non-text body>"
	}
}

func truncate(s string, n int) string {
	if n > 0 && len(s) > n {
		return s[:n] + "...(truncated)"
	}
	return s
}

// Logger implements resty.Logger for the client's own diagnostics (retries,
// redirects).
type Logger struct{ log *logger.Logger }

// NewLogger creates a resty.Logger.
func NewLogger(log *logger.Logger) *Logger {
	if log == nil {
		log = logger.Default()
	}
	return &Logger{log: log}
}

func (l *Logger) Errorf(format string, v ...any) { l.log.Errorf(strings.TrimSpace(format), v...) }
func (l *Logger) Warnf(format string, v ...any)  { l.log.Warnf(strings.TrimSpace(format), v...) }
func (l *Logger) Debugf(format string, v ...any) { l.log.Debugf(strings.TrimSpace(format), v...) }

// Transport is a shortcut for people who prefer a RoundTripper over resty
// middleware; it is httplog-compatible behaviour for any http.Client.
func Transport(log *logger.Logger, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &rt{base: base, log: log}
}

type rt struct {
	base http.RoundTripper
	log  *logger.Logger
}

func (t *rt) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(r)
	attrs := []logger.Attr{logger.String(logger.KeyMethod, r.Method), logger.String("url", r.URL.Redacted()), logger.Duration(time.Since(start))}
	if err != nil {
		t.log.LogAttrs(r.Context(), logger.LevelError, "http client request", append(attrs, logger.Err(err))...)
		return nil, err
	}
	t.log.LogAttrs(r.Context(), logger.LevelDebug, "http client request", append(attrs, logger.Int(logger.KeyStatus, resp.StatusCode))...)
	return resp, nil
}
