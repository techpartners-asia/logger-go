// Package ginlog integrates the techpartners logger with Gin.
//
//	r := gin.New()
//	r.Use(ginlog.New(log), ginlog.Recovery(log))
//
//	r.GET("/x", func(c *gin.Context) {
//		ginlog.FromCtx(c).Info("hello")
//	})
package ginlog

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/techpartners-asia/logger-go"
)

// RequestIDHeader is the correlation header.
const RequestIDHeader = "X-Request-ID"

const ctxKey = "techpartners.logger"

// Config tunes the middleware.
type Config struct {
	SkipPaths     []string
	Skip          func(c *gin.Context) bool
	SlowThreshold time.Duration
	Fields        func(c *gin.Context, status int, duration time.Duration) []logger.Attr
	Message       string
	GenerateID    func() string
}

// New returns the request-logging middleware.
func New(log *logger.Logger, cfgs ...Config) gin.HandlerFunc {
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

	return func(c *gin.Context) {
		if (cfg.Skip != nil && cfg.Skip(c)) || skipPath(cfg.SkipPaths, c.Request.URL.Path) {
			c.Next()
			return
		}
		start := time.Now()
		reqID := c.GetHeader(RequestIDHeader)
		if reqID == "" {
			reqID = cfg.GenerateID()
		}
		c.Header(RequestIDHeader, reqID)

		path := c.Request.URL.Path
		reqLog := log.WithAttrs(
			logger.String(logger.KeyRequestID, reqID),
			logger.String(logger.KeyMethod, c.Request.Method),
			logger.String(logger.KeyPath, path),
		)
		c.Set(ctxKey, reqLog)
		ctx := logger.NewContext(c.Request.Context(), reqLog)
		ctx = logger.ContextWithFields(ctx, logger.String(logger.KeyRequestID, reqID))
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		dur := time.Since(start)
		status := c.Writer.Status()
		// Emitted via the base logger: request_id arrives through the context
		// fields, so it is not duplicated by reqLog's attrs.
		attrs := []logger.Attr{
			logger.String(logger.KeyMethod, c.Request.Method),
			logger.String(logger.KeyPath, path),
			logger.Int(logger.KeyStatus, status),
			logger.Duration(dur),
			logger.Int(logger.KeyBytes, c.Writer.Size()),
			logger.String(logger.KeyRemoteAddr, c.ClientIP()),
			logger.String(logger.KeyUserAgent, c.Request.UserAgent()),
		}
		if q := c.Request.URL.RawQuery; q != "" {
			attrs = append(attrs, logger.String("query", q))
		}
		if route := c.FullPath(); route != "" && route != path {
			attrs = append(attrs, logger.String("route", route))
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, logger.String(logger.KeyError, c.Errors.String()))
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
	}
}

// Recovery logs panics with a stack trace and aborts with 500. Use it instead
// of gin.Recovery so panics end up in structured logs.
func Recovery(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				FromCtx(c).LogAttrs(c.Request.Context(), logger.LevelError, "http panic",
					logger.Any("panic", rec),
					logger.String(logger.KeyStack, string(debug.Stack())),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

// FromCtx returns the request-scoped logger, falling back to the default.
func FromCtx(c *gin.Context) *logger.Logger {
	if v, ok := c.Get(ctxKey); ok {
		if l, ok := v.(*logger.Logger); ok && l != nil {
			return l
		}
	}
	return logger.FromContext(c.Request.Context())
}

// RequestID returns the request ID for the current request.
func RequestID(c *gin.Context) string {
	if id := c.Writer.Header().Get(RequestIDHeader); id != "" {
		return id
	}
	return c.GetHeader(RequestIDHeader)
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
