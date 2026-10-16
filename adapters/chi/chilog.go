// Package chilog adapts httplog for chi routers. chi is plain net/http, so
// httplog.Middleware works as-is; this package adds the matched route
// pattern ("/users/{id}") to the request line and offers chi-flavoured
// helpers.
//
//	r := chi.NewRouter()
//	r.Use(chilog.Middleware(log))
package chilog

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/techpartners-asia/logger-go"
	"github.com/techpartners-asia/logger-go/httplog"
)

// Options is httplog.Options.
type Options = httplog.Options

// Middleware wraps httplog.Middleware and appends the chi route pattern as
// the "route" attribute.
func Middleware(log *logger.Logger, opts ...Options) func(http.Handler) http.Handler {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	userFields := o.Fields
	o.Fields = func(r *http.Request, status int, d time.Duration) []logger.Attr {
		var attrs []logger.Attr
		if rc := chi.RouteContext(r.Context()); rc != nil {
			if p := rc.RoutePattern(); p != "" && p != r.URL.Path {
				attrs = append(attrs, logger.String("route", p))
			}
		}
		if userFields != nil {
			attrs = append(attrs, userFields(r, status, d)...)
		}
		return attrs
	}
	return httplog.Middleware(log, o)
}

// FromRequest returns the request-scoped logger.
func FromRequest(r *http.Request) *logger.Logger { return logger.FromContext(r.Context()) }

// RequestID returns the request ID set by the middleware.
func RequestID(r *http.Request) string { return httplog.RequestID(r) }
