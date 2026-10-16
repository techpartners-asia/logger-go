// Package otellog adds OpenTelemetry trace correlation to the techpartners
// logger: every record logged with a context that carries an active span gets
// trace_id, span_id and trace_flags attributes.
//
//	log := logger.MustNew(
//		logger.WithService("api"),
//		otellog.WithTrace(),
//	)
//
//	log.InfoContext(ctx, "charged")   // {"trace_id":"4bf9...","span_id":"00f0...", ...}
package otellog

import (
	"context"

	"github.com/techpartners-asia/logger-go"
	"go.opentelemetry.io/otel/trace"
)

// Keys used for the injected attributes.
const (
	KeyTraceID    = logger.KeyTraceID
	KeySpanID     = logger.KeySpanID
	KeyTraceFlags = "trace_flags"
)

// Extractor is a logger.ContextExtractor that reads the span context.
func Extractor(ctx context.Context) []logger.Attr {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	attrs := []logger.Attr{
		logger.String(KeyTraceID, sc.TraceID().String()),
		logger.String(KeySpanID, sc.SpanID().String()),
	}
	if sc.IsSampled() {
		attrs = append(attrs, logger.String(KeyTraceFlags, sc.TraceFlags().String()))
	}
	return attrs
}

// WithTrace is the option form of Extractor.
func WithTrace() logger.Option {
	return logger.WithContextExtractor(Extractor)
}

// ContextWithTrace returns ctx with the span's IDs added as logger context
// fields, for code paths that log through a *slog.Logger or any logger
// without the extractor installed.
func ContextWithTrace(ctx context.Context) context.Context {
	return logger.ContextWithFields(ctx, Extractor(ctx)...)
}
