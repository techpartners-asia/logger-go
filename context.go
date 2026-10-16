package logger

import (
	"context"
	"log/slog"
)

type ctxKey int

const (
	loggerCtxKey ctxKey = iota
	fieldsCtxKey
)

// NewContext returns a copy of ctx carrying l. Retrieve it with FromContext.
func NewContext(ctx context.Context, l *Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, loggerCtxKey, l)
}

// FromContext returns the logger stored in ctx, or the package default when
// none is present. It never returns nil.
func FromContext(ctx context.Context) *Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerCtxKey).(*Logger); ok && l != nil {
			return l
		}
	}
	return Default()
}

// ContextWithFields returns a copy of ctx carrying attributes that will be
// added to every record logged with that context through the *Context
// methods, regardless of which Logger instance is used. Repeated calls
// accumulate.
//
//	ctx = logger.ContextWithFields(ctx, logger.String("request_id", id))
//	log.InfoContext(ctx, "handling")   // includes request_id
func ContextWithFields(ctx context.Context, attrs ...Attr) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(attrs) == 0 {
		return ctx
	}
	existing := FieldsFromContext(ctx)
	merged := make([]Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, fieldsCtxKey, merged)
}

// ContextWith is ContextWithFields with slog-style variadic args.
func ContextWith(ctx context.Context, args ...any) context.Context {
	return ContextWithFields(ctx, argsToAttrs(args)...)
}

// FieldsFromContext returns the attributes stored by ContextWithFields.
func FieldsFromContext(ctx context.Context) []Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(fieldsCtxKey).([]Attr)
	return attrs
}

// contextKeyMapping maps a context value key to an attribute name.
type contextKeyMapping struct {
	attr   string
	ctxKey any
}

// contextHandler injects context fields and extractor output into records.
type contextHandler struct {
	next       slog.Handler
	extractors []ContextExtractor
	ctxKeys    []contextKeyMapping
}

func newContextHandler(next slog.Handler, ex []ContextExtractor, keys []contextKeyMapping) slog.Handler {
	return &contextHandler{next: next, extractors: ex, ctxKeys: keys}
}

func (h *contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if attrs := FieldsFromContext(ctx); len(attrs) > 0 {
			r.AddAttrs(attrs...)
		}
		for _, m := range h.ctxKeys {
			if v := ctx.Value(m.ctxKey); v != nil {
				r.AddAttrs(slog.Any(m.attr, v))
			}
		}
		for _, ex := range h.extractors {
			if attrs := ex(ctx); len(attrs) > 0 {
				r.AddAttrs(attrs...)
			}
		}
	}
	return h.next.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs), extractors: h.extractors, ctxKeys: h.ctxKeys}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name), extractors: h.extractors, ctxKeys: h.ctxKeys}
}
