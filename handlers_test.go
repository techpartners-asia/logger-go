package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRedaction(t *testing.T) {
	l, buf := newJSON(t, WithRedaction("custom_field"))
	l.With("Authorization", "Bearer x").WithGroup("req").Info("login",
		"password", "hunter2",
		"user", "bob",
		"X-Api-Key", "k",
		"custom_field", 1,
		Group("card", "card_number", "4111", "exp", "12/30"),
	)
	m := lines(t, buf)[0]
	if m["Authorization"] != "[REDACTED]" {
		t.Errorf("WithAttrs not redacted: %v", m)
	}
	req := m["req"].(map[string]any)
	if req["password"] != "[REDACTED]" || req["X-Api-Key"] != "[REDACTED]" || req["custom_field"] != "[REDACTED]" {
		t.Errorf("record attrs: %v", req)
	}
	if req["user"] != "bob" {
		t.Error("over-redacted")
	}
	card := req["card"].(map[string]any)
	if card["card_number"] != "[REDACTED]" || card["exp"] != "12/30" {
		t.Errorf("nested: %v", card)
	}
}

func TestRedactionKeepLast(t *testing.T) {
	l, buf := newJSON(t, WithRedactionConfig(Redaction{KeepLast: 4}))
	l.Info("x", "card_number", "4111111111111111", "pin", "12")
	m := lines(t, buf)[0]
	if m["card_number"] != "****1111" || m["pin"] != "[REDACTED]" {
		t.Errorf("%v", m)
	}
}

func TestSampling(t *testing.T) {
	dropped := map[string]uint64{}
	l, buf := newJSON(t, WithSampling(Sampling{Initial: 2, Thereafter: 5, Tick: time.Hour,
		OnDropped: func(_ Level, msg string, n uint64) { dropped[msg] = n }}))
	for i := 0; i < 12; i++ {
		l.Info("hot")
	}
	for i := 0; i < 12; i++ {
		l.Error("err") // exempt
	}
	l.Info("other")
	ls := lines(t, buf)
	hot, errs, other := 0, 0, 0
	for _, m := range ls {
		switch m["msg"] {
		case "hot":
			hot++
		case "err":
			errs++
		case "other":
			other++
		}
	}
	// 2 initial + every 5th of the remaining 10 (5th, 10th) = 4
	if hot != 4 || errs != 12 || other != 1 {
		t.Errorf("hot=%d errs=%d other=%d", hot, errs, other)
	}
}

func TestSamplingWindowReset(t *testing.T) {
	h := NewSamplingHandler(slog.NewJSONHandler(discard{}, nil), Sampling{Initial: 1, Thereafter: 1000, Tick: time.Millisecond})
	now := time.Now()
	if !h.allow(LevelInfo, "m", now) || h.allow(LevelInfo, "m", now) {
		t.Fatal("first window")
	}
	if !h.allow(LevelInfo, "m", now.Add(2*time.Millisecond)) {
		t.Fatal("window did not reset")
	}
}

func TestSamplingConcurrent(t *testing.T) {
	h := NewSamplingHandler(slog.NewJSONHandler(discard{}, nil), Sampling{Initial: 10, Thereafter: 10, Tick: time.Hour})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				h.allow(LevelInfo, "c", time.Time{})
			}
		}()
	}
	wg.Wait()
}

func TestStackTrace(t *testing.T) {
	l, buf := newJSON(t, WithStackTrace(LevelError))
	l.Warn("no stack")
	l.Error("with stack")
	ls := lines(t, buf)
	if _, ok := ls[0]["stack"]; ok {
		t.Error("warn should have no stack")
	}
	st, _ := ls[1]["stack"].(string)
	if !strings.Contains(st, "TestStackTrace") || strings.Contains(st, "log/slog") {
		t.Errorf("stack = %q", st)
	}
}

func TestConsoleHandler(t *testing.T) {
	var buf bytes.Buffer
	l := MustNew(WithOutput(&buf), WithFormat(FormatConsole), WithColor(false), WithLevel(LevelTrace), WithService("api"))
	l.With("req_id", "abc").WithGroup("http").Info("served", "status", 200, "path", "/a b", Err(errTest))
	got := buf.String()
	for _, want := range []string{" INF served", "service=api", "req_id=abc", `http.status=200`, `http.path="/a b"`, "http.error=boom"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	buf.Reset()
	l.Trace("t")
	l.Error("e")
	if !strings.Contains(buf.String(), " TRC t") || !strings.Contains(buf.String(), " ERR e") {
		t.Errorf("%q", buf.String())
	}
}

func TestConsoleColor(t *testing.T) {
	var buf bytes.Buffer
	l := MustNew(WithOutput(&buf), WithFormat(FormatConsole), WithColor(true))
	l.Warn("w", "k", "v")
	if !strings.Contains(buf.String(), ansiYellow+"WRN"+ansiReset) || !strings.Contains(buf.String(), ansiCyan+"k"+ansiReset+"=v") {
		t.Errorf("%q", buf.String())
	}
}

func TestConsoleSource(t *testing.T) {
	var buf bytes.Buffer
	l := MustNew(WithOutput(&buf), WithFormat(FormatConsole), WithColor(false), WithSource(true))
	l.Info("x")
	if !strings.Contains(buf.String(), "logger-go/handlers_test.go:") {
		t.Errorf("%q", buf.String())
	}
}

func TestMultiAndLevelHandler(t *testing.T) {
	var all, errs bytes.Buffer
	h := NewMultiHandler(
		slog.NewJSONHandler(&all, &slog.HandlerOptions{Level: LevelDebug}),
		NewLevelHandler(LevelError, slog.NewJSONHandler(&errs, nil)),
	)
	l := MustNew(WithHandler(h), WithLevel(LevelDebug), WithService("s"))
	l.Debug("d")
	l.Error("e")
	if n := len(lines(t, &all)); n != 2 {
		t.Errorf("all=%d", n)
	}
	el := lines(t, &errs)
	if len(el) != 1 || el[0]["service"] != "s" {
		t.Errorf("errs=%v", el)
	}
}

var errTest = errorString("boom")

type errorString string

func (e errorString) Error() string { return string(e) }
