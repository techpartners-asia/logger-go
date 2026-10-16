// Package httplog provides net/http middleware and an http.RoundTripper that
// log requests with the techpartners logger, propagate request IDs, and put a
// request-scoped logger into the context.
//
// Server side:
//
//	mux := http.NewServeMux()
//	handler := httplog.Middleware(log)(mux)
//
//	// in a handler:
//	l := logger.FromContext(r.Context())      // has request_id, method, path
//	l.Info("creating order")
//
// Client side:
//
//	client := &http.Client{Transport: httplog.Transport(log, nil)}
package httplog

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/techpartners-asia/logger-go"
)

// RequestIDHeader is the header read and written for request correlation.
const RequestIDHeader = "X-Request-ID"

// Options tune Middleware.
type Options struct {
	// SkipPaths are not logged at all (health checks, metrics). Exact match or
	// prefix match when the entry ends with "*".
	SkipPaths []string
	// Skip is a free-form predicate; return true to skip logging.
	Skip func(r *http.Request) bool
	// SlowThreshold logs requests slower than this at WARN (0 disables).
	SlowThreshold time.Duration
	// LogRequestHeaders / LogResponseHeaders include headers (redaction applies
	// to Authorization, Cookie, etc. when the logger has redaction enabled).
	LogRequestHeaders  bool
	LogResponseHeaders bool
	// RequestIDHeader overrides the header name. Empty uses RequestIDHeader.
	RequestIDHeader string
	// GenerateID overrides request-ID generation (default 16 random bytes hex).
	GenerateID func() string
	// TrustProxyHeaders uses X-Forwarded-For / X-Real-IP for remote_addr.
	TrustProxyHeaders bool
	// Fields adds custom attributes per request, computed after the response.
	Fields func(r *http.Request, status int, duration time.Duration) []logger.Attr
	// Message overrides the log line text. Default "http request".
	Message string
	// Recover logs panics with a stack and returns 500 instead of crashing.
	// Enabled by default; set DisableRecover to turn it off.
	DisableRecover bool
}

// Middleware returns a net/http middleware that logs each request once it
// completes and stores a request-scoped logger in the context.
func Middleware(log *logger.Logger, opts ...Options) func(http.Handler) http.Handler {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.RequestIDHeader == "" {
		o.RequestIDHeader = RequestIDHeader
	}
	if o.GenerateID == nil {
		o.GenerateID = NewRequestID
	}
	if o.Message == "" {
		o.Message = "http request"
	}
	if log == nil {
		log = logger.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(o, r) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()

			reqID := r.Header.Get(o.RequestIDHeader)
			if reqID == "" {
				reqID = o.GenerateID()
			}
			w.Header().Set(o.RequestIDHeader, reqID)

			reqLog := log.WithAttrs(
				logger.String(logger.KeyRequestID, reqID),
				logger.String(logger.KeyMethod, r.Method),
				logger.String(logger.KeyPath, r.URL.Path),
			)
			ctx := logger.NewContext(r.Context(), reqLog)
			ctx = logger.ContextWithFields(ctx, logger.String(logger.KeyRequestID, reqID))
			r = r.WithContext(ctx)

			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			defer func() {
				var panicked any
				if !o.DisableRecover {
					panicked = recover()
				}
				dur := time.Since(start)

				// Emitted via the base logger: request_id arrives through the
				// context fields, so it is not duplicated by reqLog's attrs.
				attrs := []logger.Attr{
					logger.String(logger.KeyMethod, r.Method),
					logger.String(logger.KeyPath, r.URL.Path),
					logger.Int(logger.KeyStatus, rw.status),
					logger.Duration(dur),
					logger.Int64(logger.KeyBytes, rw.bytes),
					logger.String(logger.KeyRemoteAddr, remoteAddr(r, o.TrustProxyHeaders)),
					logger.String(logger.KeyUserAgent, r.UserAgent()),
				}
				if q := r.URL.RawQuery; q != "" {
					attrs = append(attrs, logger.String("query", q))
				}
				if o.LogRequestHeaders {
					attrs = append(attrs, headersAttr("request_headers", r.Header))
				}
				if o.LogResponseHeaders {
					attrs = append(attrs, headersAttr("response_headers", w.Header()))
				}
				if o.Fields != nil {
					attrs = append(attrs, o.Fields(r, rw.status, dur)...)
				}

				if panicked != nil {
					if !rw.wroteHeader {
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
						rw.status = http.StatusInternalServerError
						attrs[2] = logger.Int(logger.KeyStatus, rw.status)
					}
					attrs = append(attrs,
						logger.Any("panic", panicked),
						logger.String(logger.KeyStack, string(debug.Stack())),
					)
					log.LogAttrs(ctx, logger.LevelError, "http panic", attrs...)
					return
				}

				level := logger.LevelInfo
				switch {
				case rw.status >= 500:
					level = logger.LevelError
				case rw.status >= 400:
					level = logger.LevelWarn
				case o.SlowThreshold > 0 && dur > o.SlowThreshold:
					level = logger.LevelWarn
					attrs = append(attrs, logger.Bool("slow", true))
				}
				log.LogAttrs(ctx, level, o.Message, attrs...)
			}()

			next.ServeHTTP(rw, r)
		})
	}
}

// RequestID returns the request ID stored in the context by Middleware, if any.
func RequestID(r *http.Request) string {
	for _, a := range logger.FieldsFromContext(r.Context()) {
		if a.Key == logger.KeyRequestID {
			return a.Value.String()
		}
	}
	return r.Header.Get(RequestIDHeader)
}

// NewRequestID returns a 32-character random hex string.
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

func shouldSkip(o Options, r *http.Request) bool {
	if o.Skip != nil && o.Skip(r) {
		return true
	}
	p := r.URL.Path
	for _, s := range o.SkipPaths {
		if strings.HasSuffix(s, "*") {
			if strings.HasPrefix(p, strings.TrimSuffix(s, "*")) {
				return true
			}
		} else if p == s {
			return true
		}
	}
	return false
}

func remoteAddr(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
		if rip := r.Header.Get("X-Real-IP"); rip != "" {
			return rip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func headersAttr(key string, h http.Header) logger.Attr {
	attrs := make([]any, 0, len(h))
	for k, v := range h {
		attrs = append(attrs, logger.String(k, strings.Join(v, ", ")))
	}
	return logger.Group(key, attrs...)
}

// responseWriter captures status and size while preserving optional
// interfaces (Flusher, Hijacker) that frameworks rely on.
type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		if !w.wroteHeader {
			w.WriteHeader(http.StatusOK)
		}
		f.Flush()
	}
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("httplog: underlying ResponseWriter does not support hijacking")
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// --- client --------------------------------------------------------------

// TransportOptions tune Transport.
type TransportOptions struct {
	// Message overrides the log text. Default "http client request".
	Message string
	// PropagateRequestID copies the request ID from the context (set by
	// Middleware) into the outgoing X-Request-ID header. Default true unless
	// DisablePropagation is set.
	DisablePropagation bool
	// Level for successful calls. Default DEBUG, so production stays quiet
	// unless you raise it.
	Level logger.Level
}

// Transport returns an http.RoundTripper that logs outbound requests (method,
// url, status, duration) and forwards the request ID. base nil means
// http.DefaultTransport.
func Transport(log *logger.Logger, base http.RoundTripper, opts ...TransportOptions) http.RoundTripper {
	var o TransportOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Message == "" {
		o.Message = "http client request"
	}
	if o.Level == 0 {
		o.Level = logger.LevelDebug
	}
	if base == nil {
		base = http.DefaultTransport
	}
	if log == nil {
		log = logger.Default()
	}
	return &loggingTransport{base: base, log: log, o: o}
}

type loggingTransport struct {
	base http.RoundTripper
	log  *logger.Logger
	o    TransportOptions
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if !t.o.DisablePropagation && req.Header.Get(RequestIDHeader) == "" {
		if id := RequestID(req); id != "" {
			req = req.Clone(ctx)
			req.Header.Set(RequestIDHeader, id)
		}
	}
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	dur := time.Since(start)

	attrs := []logger.Attr{
		logger.String(logger.KeyMethod, req.Method),
		logger.String("url", req.URL.Redacted()),
		logger.Duration(dur),
	}
	if err != nil {
		attrs = append(attrs, logger.Err(err))
		t.log.LogAttrs(ctx, logger.LevelError, t.o.Message, attrs...)
		return nil, err
	}
	attrs = append(attrs, logger.Int(logger.KeyStatus, resp.StatusCode))
	level := t.o.Level
	if resp.StatusCode >= 500 {
		level = logger.LevelError
	} else if resp.StatusCode >= 400 {
		level = logger.LevelWarn
	}
	t.log.LogAttrs(ctx, level, t.o.Message, attrs...)
	return resp, nil
}
