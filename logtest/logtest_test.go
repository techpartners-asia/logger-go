package logtest_test

import (
	"testing"

	"github.com/techpartners-asia/logger-go"
	"github.com/techpartners-asia/logger-go/logtest"
)

func TestRecorder(t *testing.T) {
	log, rec := logtest.New()
	log.With("svc", "a").WithGroup("g").Warn("careful", "n", 2, logger.Group("inner", "x", true))
	log.Named("child").Debug("dbg")

	if rec.Len() != 2 || !rec.Has(logger.LevelWarn, "care") {
		t.Fatalf("entries: %+v", rec.Entries())
	}
	e := rec.Entries()[0]
	if e.Attrs["svc"] != "a" {
		t.Errorf("svc: %v", e.Attrs)
	}
	if v, _ := e.Get("g.n"); v != int64(2) {
		t.Errorf("g.n = %v", v)
	}
	if v, _ := e.Get("g.inner.x"); v != true {
		t.Errorf("g.inner.x = %v", v)
	}
	if rec.Last().String("logger") != "child" || rec.Last().Level != logger.LevelDebug {
		t.Errorf("last = %+v", rec.Last())
	}
	if rec.Messages()[0] != "careful" {
		t.Error("messages")
	}
	rec.Reset()
	if rec.Len() != 0 {
		t.Error("reset")
	}
}
