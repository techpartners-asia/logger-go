// Package echolog integrates the techpartners logger with Echo v4.
//
//	e := echo.New()
//	e.Use(echolog.New(log))
//
//	e.GET("/x/:id", func(c echo.Context) error {
//		echolog.FromCtx(c).Info("hit")
//		return c.String(200, "ok")
//	})
package echolog

import (
	"crypto/rand"
	"encoding/hex"
	"runtime/debug"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/techpartners-asia/logger-go"
)

// RequestIDHeader is the correlation header.
const RequestIDHeader = echo.HeaderXRequestID

const ctxKey = "techpartners.logger"

// Config tunes the middleware.
type Config struct {
	Skipper       func(c echo.Context) bool
	SkipPaths     []string
	SlowThreshold time.Duration
	Fields        func(c echo.Context, status int, duration time.Duration) []logger.Attr
	Message       string
	GenerateID    func() string
	// DisableRecover turns off panic recovery.
	DisableRecover bool
}

// New returns the request-logging middleware. It also recovers panics
// (logged as "http panic", answered with 500), so echo's Recover middleware
// is not required.
func New(log *logger.Logger, cfgs ...Config) echo.MiddlewareFunc {
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if log == nil {
		log = logger.Default()
	}
	if cfg.Message == "" {
		cfg.Message = "http request"
	}
	if cfg.GenerateID == nil {
		cfg.GenerateID = newRequestID
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			req := c.Request()
			if (cfg.Skipper != nil && cfg.Skipper(c)) || skipPath(cfg.SkipPaths, req.URL.Path) {
				return next(c)
			}
			start := time.Now()
			reqID := req.Header.Get(RequestIDHeader)
			if reqID == "" {
				reqID = cfg.GenerateID()
			}
			c.Response().Header().Set(RequestIDHeader, reqID)

			path := req.URL.Path
			reqLog := log.WithAttrs(
				logger.String(logger.KeyRequestID, reqID),
				logger.String(logger.KeyMethod, req.Method),
				logger.String(logger.KeyPath, path),
			)
			c.Set(ctxKey, reqLog)
			ctx := logger.NewContext(req.Context(), reqLog)
			ctx = logger.ContextWithFields(ctx, logger.String(logger.KeyRequestID, reqID))
			req = req.WithContext(ctx)
			c.SetRequest(req)

			defer func() {
				if cfg.DisableRecover {
					return
				}
				if r := recover(); r != nil {
					log.LogAttrs(ctx, logger.LevelError, "http panic",
						logger.String(logger.KeyMethod, req.Method),
						logger.String(logger.KeyPath, path),
						logger.Int(logger.KeyStatus, 500),
						logger.Duration(time.Since(start)),
						logger.Any("panic", r),
						logger.String(logger.KeyStack, string(debug.Stack())),
					)
					err = echo.ErrInternalServerError
					c.Error(err)
				}
			}()

			if err = next(c); err != nil {
				c.Error(err) // lets the HTTPErrorHandler write the status
			}

			dur := time.Since(start)
			res := c.Response()
			status := res.Status
			attrs := []logger.Attr{
				logger.String(logger.KeyMethod, req.Method),
				logger.String(logger.KeyPath, path),
				logger.Int(logger.KeyStatus, status),
				logger.Duration(dur),
				logger.Int64(logger.KeyBytes, res.Size),
				logger.String(logger.KeyRemoteAddr, c.RealIP()),
				logger.String(logger.KeyUserAgent, req.UserAgent()),
			}
			if q := req.URL.RawQuery; q != "" {
				attrs = append(attrs, logger.String("query", q))
			}
			if route := c.Path(); route != "" && route != path {
				attrs = append(attrs, logger.String("route", route))
			}
			if err != nil {
				attrs = append(attrs, logger.Err(err))
			}
			if cfg.Fields != nil {
				attrs = append(attrs, cfg.Fields(c, status, dur)...)
			}
			level := logger.LevelInfo
			switch {
			case status >= 500:
				level = logger.LevelError
			case status >= 400:
				level = logger.LevelWarn
			case cfg.SlowThreshold > 0 && dur > cfg.SlowThreshold:
				level = logger.LevelWarn
				attrs = append(attrs, logger.Bool("slow", true))
			}
			log.LogAttrs(ctx, level, cfg.Message, attrs...)
			return nil // already handled by c.Error
		}
	}
}

// FromCtx returns the request-scoped logger, falling back to the default.
func FromCtx(c echo.Context) *logger.Logger {
	if l, ok := c.Get(ctxKey).(*logger.Logger); ok && l != nil {
		return l
	}
	return logger.FromContext(c.Request().Context())
}

// RequestID returns the request ID for the current request.
func RequestID(c echo.Context) string {
	if id := c.Response().Header().Get(RequestIDHeader); id != "" {
		return id
	}
	return c.Request().Header.Get(RequestIDHeader)
}

func skipPath(skips []string, p string) bool {
	for _, s := range skips {
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

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
