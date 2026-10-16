package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFromLookup(t *testing.T) {
	env := map[string]string{
		"LOG_LEVEL": "debug", "LOG_FORMAT": "console", "SERVICE_NAME": "orders",
		"APP_ENV": "staging", "APP_VERSION": "v9", "LOG_SOURCE": "true",
		"LOG_REDACT_KEYS": "foo, bar", "LOG_SAMPLING_INITIAL": "5", "NO_COLOR": "1",
		"LOG_STACK_TRACE_LEVEL": "error", "LOG_KEYS": "datadog",
	}
	c := ConfigFromLookup(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if c.Level != "debug" || c.Format != "console" || c.Service != "orders" || c.Env != "staging" || c.Version != "v9" || !c.Source {
		t.Errorf("%+v", c)
	}
	if !c.Redact || len(c.RedactKeys) != 2 || c.RedactKeys[1] != "bar" {
		t.Errorf("redact: %+v", c)
	}
	if c.Color == nil || *c.Color {
		t.Error("NO_COLOR")
	}

	var buf bytes.Buffer
	l := MustNew(WithConfig(c), WithOutput(&buf), WithFormat(FormatJSON))
	l.Debug("x", "foo", "secret", "token", "t")
	m := lines(t, &buf)[0]
	if m["status"] != "DEBUG" || m["message"] != "x" || m["service"] != "orders" || m["foo"] != "[REDACTED]" || m["token"] != "[REDACTED]" {
		t.Errorf("%v", m)
	}
	if l.Level() != LevelDebug {
		t.Error("level")
	}
}

func TestConfigFileOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	l, err := New(WithConfig(Config{Output: path}))
	if err != nil {
		t.Fatal(err)
	}
	l.Info("to file")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !bytes.Contains(b, []byte(`"msg":"to file"`)) {
		t.Errorf("file = %q", b)
	}
}

func TestConfigBadFile(t *testing.T) {
	if _, err := New(WithConfig(Config{Output: "/nonexistent-dir/x/y.log"})); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{"TRACE": LevelTrace, "warning": LevelWarn, "ERROR": LevelError, "fatal": LevelFatal, "": LevelInfo, "INFO+2": LevelInfo + 2}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("expected error")
	}
}
