// Package fiberlog integrates the techpartners logger with Fiber v3.
//
//	app := fiber.New()
//	app.Use(fiberlog.New(log))
//
//	app.Get("/orders/:id", func(c fiber.Ctx) error {
//		l := fiberlog.FromCtx(c)             // request-scoped logger
//		l.Info("fetching order", "id", c.Params("id"))
//		return c.JSON(order)
//	})
//
// The middleware sets X-Request-ID, stores the logger in both Fiber locals and
// the request context (so logger.FromContext(c.Context()) also works), and
// logs one line per request with status, duration and size.
package fiberlog

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/techpartners-asia/logger-go"
)

// RequestIDHeader is the correlation header.
const RequestIDHeader = "X-Request-ID"

const localsKey = "techpartners.logger"

// Config tunes the middleware.
type Config struct {
	// Next skips the middleware when it returns true.
	Next func(c fiber.Ctx) bool
	// SkipPaths are not logged. Exact match or prefix when ending with "*".
	SkipPaths []string
	// SlowThreshold logs slower requests at WARN.
	SlowThreshold time.Duration
	// LogRequestHeaders includes request headers (redaction applies).
	LogRequestHeaders bool
	// Fields appends custom attributes after the handler ran.
	Fields func(c fiber.Ctx, status int, duration time.Duration) []logger.Attr
	// Message overrides the log text. Default "http request".
	Message string
	// GenerateID overrides request-ID generation.
	GenerateID func() string
	// Logger overrides the logger passed to New.
	Logger *logger.Logger
}

// New returns a Fiber middleware. Register it before other middleware so
// every handler sees the request logger.
func New(log *logger.Logger, cfgs ...Config) fiber.Handler {
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.Logger != nil {
		log = cfg.Logger
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

	return func(c fiber.Ctx) (err error) {
		if (cfg.Next != nil && cfg.Next(c)) || skipPath(cfg.SkipPaths, c.Path()) {
			return c.Next()
		}
		start := time.Now()

		reqID := c.Get(RequestIDHeader)
		if reqID == "" {
			reqID = cfg.GenerateID()
		}
		c.Set(RequestIDHeader, reqID)

		method, path := c.Method(), c.Path()
		reqLog := log.WithAttrs(
			logger.String(logger.KeyRequestID, reqID),
			logger.String(logger.KeyMethod, method),
			logger.String(logger.KeyPath, path),
		)
		c.Locals(localsKey, reqLog)
		ctx := logger.NewContext(c.Context(), reqLog)
		ctx = logger.ContextWithFields(ctx, logger.String(logger.KeyRequestID, reqID))
		c.SetContext(ctx)

		defer func() {
			if rec := recover(); rec != nil {
				dur := time.Since(start)
				attrs := []logger.Attr{
					logger.String(logger.KeyMethod, method),
					logger.String(logger.KeyPath, path),
					logger.Int(logger.KeyStatus, fiber.StatusInternalServerError),
					logger.Duration(dur),
					logger.String(logger.KeyRemoteAddr, c.IP()),
					logger.Any("panic", rec),
					logger.String(logger.KeyStack, string(debug.Stack())),
				}
				log.LogAttrs(ctx, logger.LevelError, "http panic", attrs...)
				err = fiber.ErrInternalServerError
				_ = c.Status(fiber.StatusInternalServerError).SendString(fiber.ErrInternalServerError.Message)
			}
		}()

		err = c.Next()
		dur := time.Since(start)

		status := c.Response().StatusCode()
		if err != nil {
			var fe *fiber.Error
			if errors.As(err, &fe) {
				status = fe.Code
			} else if status < 400 {
				status = fiber.StatusInternalServerError
			}
		}

		// Emitted via the base logger: request_id arrives through the context
		// fields, so it is not duplicated by reqLog's attrs.
		attrs := []logger.Attr{
			logger.String(logger.KeyMethod, method),
			logger.String(logger.KeyPath, path),
			logger.Int(logger.KeyStatus, status),
			logger.Duration(dur),
			logger.Int(logger.KeyBytes, len(c.Response().Body())),
			logger.String(logger.KeyRemoteAddr, c.IP()),
			logger.String(logger.KeyUserAgent, c.Get(fiber.HeaderUserAgent)),
		}
		if q := c.Request().URI().QueryString(); len(q) > 0 {
			attrs = append(attrs, logger.String("query", string(q)))
		}
		if cfg.LogRequestHeaders {
			var hs []any
			for k, v := range c.GetReqHeaders() {
				hs = append(hs, logger.String(k, strings.Join(v, ", ")))
			}
			attrs = append(attrs, logger.Group("request_headers", hs...))
		}
		if cfg.Fields != nil {
			attrs = append(attrs, cfg.Fields(c, status, dur)...)
		}
		if err != nil {
			attrs = append(attrs, logger.Err(err))
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
		return err
	}
}

// FromCtx returns the request-scoped logger, or the default logger when the
// middleware is not installed.
func FromCtx(c fiber.Ctx) *logger.Logger {
	if l, ok := c.Locals(localsKey).(*logger.Logger); ok && l != nil {
		return l
	}
	return logger.FromContext(c.Context())
}

// RequestID returns the request ID for the current request.
func RequestID(c fiber.Ctx) string {
	return c.GetRespHeader(RequestIDHeader, c.Get(RequestIDHeader))
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
