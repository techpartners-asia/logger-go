package logger

import (
	"context"
	"testing"
)

type userKey struct{}

func TestContextFieldsAndExtractors(t *testing.T) {
	l, buf := newJSON(t,
		WithContextValue("user_id", userKey{}),
		WithContextExtractor(func(ctx context.Context) []Attr {
			if ctx.Value("tenant") != nil {
				return []Attr{String("tenant", ctx.Value("tenant").(string))}
			}
			return nil
		}),
	)
	ctx := ContextWithFields(context.Background(), String("request_id", "r1"))
	ctx = ContextWith(ctx, "step", 2)
	ctx = context.WithValue(ctx, userKey{}, 42)
	ctx = context.WithValue(ctx, "tenant", "acme") //nolint:staticcheck

	l.InfoContext(ctx, "x")
	l.Info("no ctx")
	ls := lines(t, buf)
	m := ls[0]
	if m["request_id"] != "r1" || m["step"] != float64(2) || m["user_id"] != float64(42) || m["tenant"] != "acme" {
		t.Errorf("ctx fields: %v", m)
	}
	if _, ok := ls[1]["request_id"]; ok {
		t.Error("fields leaked without ctx")
	}
}

func TestLoggerInContext(t *testing.T) {
	l, _ := newJSON(t)
	ctx := NewContext(context.Background(), l)
	if FromContext(ctx) != l {
		t.Error("FromContext")
	}
	if FromContext(context.Background()) != Default() {
		t.Error("fallback to default")
	}
	if FromContext(nil) == nil { //nolint:staticcheck
		t.Error("nil ctx")
	}
}
