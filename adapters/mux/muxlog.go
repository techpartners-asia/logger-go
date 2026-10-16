// Package muxlog adapts httplog for gorilla/mux. mux is plain net/http, so
// httplog.Middleware works as-is; this package adds the matched route
// template ("/users/{id}") and route name to the request line.
//
//	r := mux.NewRouter()
//	r.Use(muxlog.Middleware(log))
package muxlog

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/techpartners-asia/logger-go"
	"github.com/techpartners-asia/logger-go/httplog"
)

// Options is httplog.Options.
type Options = httplog.Options

// Middleware wraps httplog.Middleware and appends "route" (path template)
// and "route_name" (when the route is named).
func Middleware(log *logger.Logger, opts ...Options) mux.MiddlewareFunc {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	userFields := o.Fields
	o.Fields = func(r *http.Request, status int, d time.Duration) []logger.Attr {
		var attrs []logger.Attr
		if route := mux.CurrentRoute(r); route != nil {
			if tpl, err := route.GetPathTemplate(); err == nil && tpl != "" && tpl != r.URL.Path {
				attrs = append(attrs, logger.String("route", tpl))
			}
			if n := route.GetName(); n != "" {
				attrs = append(attrs, logger.String("route_name", n))
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
