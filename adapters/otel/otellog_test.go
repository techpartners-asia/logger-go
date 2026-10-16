package otellog_test

import (
	"context"
	"testing"

	otellog "github.com/techpartners-asia/logger-go/adapters/otel"
	"github.com/techpartners-asia/logger-go/logtest"
	"go.opentelemetry.io/otel/trace"
)

func TestExtractor(t *testing.T) {
	log, rec := logtest.New(otellog.WithTrace())
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	log.InfoContext(ctx, "traced")
	log.Info("untraced")
	es := rec.Entries()
	if es[0].String("trace_id") != tid.String() || es[0].String("span_id") != sid.String() || es[0].String("trace_flags") != "01" {
		t.Errorf("%+v", es[0])
	}
	if _, ok := es[1].Get("trace_id"); ok {
		t.Error("leaked")
	}
	if n := len(otellog.Extractor(context.Background())); n != 0 {
		t.Error("no span should yield nothing")
	}
}
