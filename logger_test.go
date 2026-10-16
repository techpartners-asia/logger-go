package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newJSON(t *testing.T, opts ...Option) (*Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	all := append([]Option{WithOutput(&buf), WithFormat(FormatJSON), WithLevel(LevelTrace)}, opts...)
	l, err := New(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l, &buf
}

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("bad json %q: %v", ln, err)
		}
		out = append(out, m)
	}
	return out
}

func TestJSONBasics(t *testing.T) {
	l, buf := newJSON(t, WithService("svc"), WithEnv("test"), WithVersion("1.2.3"))
	l.Info("hello", "a", 1, String("b", "x"))
	ls := lines(t, buf)
	if len(ls) != 1 {
		t.Fatalf("want 1 line, got %d", len(ls))
	}
	m := ls[0]
	for k, want := range map[string]any{"level": "INFO", "msg": "hello", "service": "svc", "env": "test", "version": "1.2.3", "a": float64(1), "b": "x"} {
		if m[k] != want {
			t.Errorf("%s = %v, want %v", k, m[k], want)
		}
	}
	if _, err := time.Parse(defaultTimeFormat, m["time"].(string)); err != nil {
		t.Errorf("time format: %v", err)
	}
}

func TestExtendedLevelNames(t *testing.T) {
	l, buf := newJSON(t)
	l.Trace("t")
	l.Debug("d")
	l.Warn("w")
	l.Error("e")
	l.Log(context.Background(), LevelFatal, "f") // Log doesn't exit
	got := []string{}
	for _, m := range lines(t, buf) {
		got = append(got, m["level"].(string))
	}
	want := []string{"TRACE", "DEBUG", "WARN", "ERROR", "FATAL"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("levels = %v, want %v", got, want)
	}
}

func TestSetLevelAffectsDerived(t *testing.T) {
	l, buf := newJSON(t, WithLevel(LevelInfo))
	child := l.With("k", "v").Named("child")
	child.Debug("hidden")
	l.SetLevel(LevelDebug)
	child.Debug("shown")
	ls := lines(t, buf)
	if len(ls) != 1 || ls[0]["msg"] != "shown" || ls[0]["logger"] != "child" || ls[0]["k"] != "v" {
		t.Fatalf("unexpected: %v", ls)
	}
	if !l.Enabled(context.Background(), LevelDebug) || l.Enabled(context.Background(), LevelTrace) {
		t.Error("Enabled mismatch")
	}
}

func TestNamedNests(t *testing.T) {
	l, buf := newJSON(t)
	l.Named("http").Named("client").Info("x")
	if got := lines(t, buf)[0]["logger"]; got != "http.client" {
		t.Errorf("logger = %v", got)
	}
}

func TestWithGroupAndErr(t *testing.T) {
	l, buf := newJSON(t)
	err := errors.New("boom")
	l.WithGroup("req").With("id", 7).Error("failed", Err(err), Err(nil))
	m := lines(t, buf)[0]
	req, _ := m["req"].(map[string]any)
	if req["id"] != float64(7) || req["error"] != "boom" {
		t.Errorf("group = %v", m)
	}
}

func TestSourceIsCaller(t *testing.T) {
	l, buf := newJSON(t, WithSource(true))
	l.Info("x")
	src, _ := lines(t, buf)[0]["source"].(string)
	if !strings.Contains(src, "logger_test.go:") {
		t.Errorf("source = %q", src)
	}
	buf.Reset()
	Default() // package-level path
	d := Default()
	SetDefault(l)
	defer SetDefault(d)
	Info("y")
	src, _ = lines(t, buf)[0]["source"].(string)
	if !strings.Contains(src, "logger_test.go:") {
		t.Errorf("pkg source = %q", src)
	}
}

func TestFatalUsesExitFunc(t *testing.T) {
	code := -1
	l, buf := newJSON(t, WithExitFunc(func(c int) { code = c }))
	l.Fatal("bye", "x", 1)
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if lines(t, buf)[0]["level"] != "FATAL" {
		t.Error("level")
	}
}

func TestPanic(t *testing.T) {
	l, _ := newJSON(t)
	defer func() {
		if r := recover(); r != "p" {
			t.Errorf("recover = %v", r)
		}
	}()
	l.Panic("p")
}

func TestPrintf(t *testing.T) {
	l, buf := newJSON(t)
	l.Infof("n=%d", 3)
	if lines(t, buf)[0]["msg"] != "n=3" {
		t.Error("printf")
	}
}

func TestKeyNamesAndUTC(t *testing.T) {
	l, buf := newJSON(t, WithKeyNames(ElasticKeyNames), WithUTC(), WithTimeFormat(time.RFC3339))
	l.Info("m")
	m := lines(t, buf)[0]
	if m["message"] != "m" || m["log.level"] != "INFO" {
		t.Errorf("keys: %v", m)
	}
	if ts := m["@timestamp"].(string); !strings.HasSuffix(ts, "Z") {
		t.Errorf("not utc: %s", ts)
	}
}

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	l := MustNew(WithOutput(&buf), WithFormat(FormatText))
	l.Info("hi", "k", "v w")
	if !strings.Contains(buf.String(), `level=INFO msg=hi k="v w"`) {
		t.Errorf("text = %q", buf.String())
	}
}

func TestSlogInterop(t *testing.T) {
	l, buf := newJSON(t)
	sl := l.Slog()
	sl.Info("via slog", "n", 1)
	if lines(t, buf)[0]["n"] != float64(1) {
		t.Error("slog interop")
	}
	buf.Reset()
	wrapped := FromSlog(slog.New(slog.NewJSONHandler(buf, nil)))
	wrapped.Info("w")
	if lines(t, buf)[0]["msg"] != "w" {
		t.Error("FromSlog")
	}
}

func TestMiddlewareOrder(t *testing.T) {
	var order []string
	mk := func(name string) Middleware {
		return func(next slog.Handler) slog.Handler {
			return &spyHandler{Handler: next, name: name, order: &order}
		}
	}
	l, _ := newJSON(t, WithMiddleware(mk("outer"), mk("inner")))
	l.Info("x")
	if strings.Join(order, ",") != "outer,inner" {
		t.Errorf("order = %v", order)
	}
}

type spyHandler struct {
	slog.Handler
	name  string
	order *[]string
}

func (s *spyHandler) Handle(ctx context.Context, r slog.Record) error {
	*s.order = append(*s.order, s.name)
	return s.Handler.Handle(ctx, r)
}

func TestNopAndClose(t *testing.T) {
	l := NewNop()
	l.Error("nothing")
	if err := l.Close(); err != nil {
		t.Error(err)
	}
	if err := l.Close(); err != nil {
		t.Error("second close", err)
	}
}

func TestProcessInfo(t *testing.T) {
	l, buf := newJSON(t, WithProcessInfo())
	l.Info("x")
	m := lines(t, buf)[0]
	if m["pid"] == nil {
		t.Error("pid missing")
	}
}

func BenchmarkInfoDisabled(b *testing.B) {
	l := MustNew(WithOutput(discard{}), WithLevel(LevelError))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Info("x", "a", 1, "b", "c")
	}
}

func BenchmarkInfoJSON(b *testing.B) {
	l := MustNew(WithOutput(discard{}), WithService("s"))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Info("request done", "status", 200, "path", "/x", "dur", 1.5)
	}
}

func BenchmarkInfoJSONRedactSample(b *testing.B) {
	l := MustNew(WithOutput(discard{}), WithRedaction(), WithSampling(Sampling{Initial: 1 << 30}))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l.Info("request done", "status", 200, "path", "/x", "token", "abc")
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
