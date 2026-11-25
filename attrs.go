package logger

import (
	"log/slog"
	"slices"
	"time"
)

// Attr is an alias of slog.Attr.
type Attr = slog.Attr

// Value is an alias of slog.Value.
type Value = slog.Value

// Well-known attribute keys used by this package and its adapters. Using the
// same keys everywhere keeps dashboards and alerts consistent across services.
const (
	KeyError      = "error"
	KeyErrorType  = "error_type"
	KeyStack      = "stack"
	KeyService    = "service"
	KeyEnv        = "env"
	KeyVersion    = "version"
	KeyHost       = "host"
	KeyPID        = "pid"
	KeyLogger     = "logger"
	KeyRequestID  = "request_id"
	KeyTraceID    = "trace_id"
	KeySpanID     = "span_id"
	KeyUserID     = "user_id"
	KeyDuration   = "duration_ms"
	KeyMethod     = "method"
	KeyPath       = "path"
	KeyStatus     = "status"
	KeyRemoteAddr = "remote_addr"
	KeyUserAgent  = "user_agent"
	KeyBytes      = "bytes"

	// Audit keys: who did what, and how it ended. See docs/logging-policy.md.
	KeyAction    = "action"
	KeyOutcome   = "outcome"
	KeyErrorCode = "error_code"
)

// Values for KeyOutcome.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
)

// Err returns an attribute for an error under the "error" key. A nil error
// yields an empty attribute which slog omits from output.
func Err(err error) Attr {
	if err == nil {
		return Attr{}
	}
	return slog.String(KeyError, err.Error())
}

// NamedErr is like Err but with a custom key.
func NamedErr(key string, err error) Attr {
	if err == nil {
		return Attr{}
	}
	return slog.String(key, err.Error())
}

// Duration records a duration in integer milliseconds under "duration_ms".
// Milliseconds as a number are far easier to aggregate in log backends than
// Go's "1.234s" strings.
func Duration(d time.Duration) Attr {
	return slog.Float64(KeyDuration, float64(d.Microseconds())/1000)
}

// Thin re-exports of slog's attribute constructors so most callers never need
// to import log/slog directly.

func String(key, value string) Attr          { return slog.String(key, value) }
func Int(key string, value int) Attr         { return slog.Int(key, value) }
func Int64(key string, value int64) Attr     { return slog.Int64(key, value) }
func Uint64(key string, value uint64) Attr   { return slog.Uint64(key, value) }
func Float64(key string, value float64) Attr { return slog.Float64(key, value) }
func Bool(key string, value bool) Attr       { return slog.Bool(key, value) }
func Time(key string, value time.Time) Attr  { return slog.Time(key, value) }
func Any(key string, value any) Attr         { return slog.Any(key, value) }
func Group(key string, args ...any) Attr     { return slog.Group(key, args...) }

// argsToAttrs converts slog-style variadic args ("key", value, Attr, ...) into
// a slice of Attr using slog's own parsing rules.
func argsToAttrs(args []any) []Attr {
	if len(args) == 0 {
		return nil
	}
	var r slog.Record
	r.Add(promoteErrors(args)...)
	attrs := make([]Attr, 0, r.NumAttrs())
	r.Attrs(func(a Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
}

// promoteErrors turns an error passed where a key is expected into Err(err),
// so log.Error("charge failed", err) prints error="...". args is copied only
// when it holds such an error.
func promoteErrors(args []any) []any {
	var out []any
	for i := 0; i < len(args); i++ {
		switch a := args[i].(type) {
		case string:
			i++ // skip the value that follows a key
		case error:
			if out == nil {
				out = slices.Clone(args)
			}
			out[i] = Err(a)
		}
	}
	if out == nil {
		return args
	}
	return out
}
