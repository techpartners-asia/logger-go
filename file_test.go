package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWithDirDefaultsToServiceName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	l, err := New(WithDir(dir), WithService("orders api"))
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hello file")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "orders-api.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"msg":"hello file"`) {
		t.Errorf("content = %s", b)
	}
}

func TestFileTee(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	l := MustNew(WithOutput(&stderr), WithFile(FileConfig{Dir: dir, Filename: "x.log", Tee: true}))
	l.Info("both")
	l.Close()
	b, _ := os.ReadFile(filepath.Join(dir, "x.log"))
	if !strings.Contains(string(b), "both") || !strings.Contains(stderr.String(), "both") {
		t.Errorf("file=%q stderr=%q", b, stderr.String())
	}
}

func TestRotationBySizeAndBackups(t *testing.T) {
	dir := t.TempDir()
	w, err := NewRotatingWriter(FileConfig{Dir: dir, Filename: "app.log", MaxSizeMB: 1, MaxBackups: 2})
	if err != nil {
		t.Fatal(err)
	}
	w.maxBytes = 100 // shrink for the test
	line := []byte(strings.Repeat("x", 40) + "\n")
	for i := 0; i < 12; i++ { // 12*41 bytes -> rotations every 2 lines
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond) // distinct backup names / mod times
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// active file + at most MaxBackups backups
	if len(names) != 3 {
		t.Errorf("files = %v", names)
	}
	active, _ := os.ReadFile(filepath.Join(dir, "app.log"))
	if len(active) == 0 || len(active) > 100 {
		t.Errorf("active size %d", len(active))
	}
}

func TestRotationCompressAndMaxAge(t *testing.T) {
	dir := t.TempDir()
	// plant an old backup that should be removed by age
	old := filepath.Join(dir, "app-20200101T000000.000.log")
	os.WriteFile(old, []byte("old"), 0o644)
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(old, past, past)

	w, err := NewRotatingWriter(FileConfig{Dir: dir, Filename: "app.log", Compress: true, MaxAgeDays: 1, MaxBackups: -1})
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("first\n"))
	if err := w.Rotate(); err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("second\n"))
	w.Close() // waits for background compression

	entries, _ := os.ReadDir(dir)
	var gz, plainBackups int
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".gz"):
			gz++
		case e.Name() != "app.log":
			plainBackups++
		}
	}
	if gz != 1 || plainBackups != 0 {
		t.Errorf("gz=%d plain=%d entries=%v", gz, plainBackups, entries)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old backup not removed")
	}
}

func TestDailyRotation(t *testing.T) {
	dir := t.TempDir()
	w, err := NewRotatingWriter(FileConfig{Dir: dir, Filename: "d.log", Daily: true})
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("yesterday\n"))
	w.openDay-- // pretend the file was opened yesterday
	w.Write([]byte("today\n"))
	w.Close()
	b, _ := os.ReadFile(filepath.Join(dir, "d.log"))
	if strings.TrimSpace(string(b)) != "today" {
		t.Errorf("active = %q", b)
	}
	if len(w.backups()) != 1 {
		t.Errorf("backups = %v", w.backups())
	}
}

func TestConfigDirFromEnv(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"LOG_DIR": dir, "LOG_FILE": "svc.log", "LOG_FILE_COMPRESS": "true", "LOG_FILE_MAX_SIZE_MB": "5"}
	c := ConfigFromLookup(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if c.Dir != dir || c.File.Filename != "svc.log" || !c.File.Compress || c.File.MaxSizeMB != 5 {
		t.Fatalf("%+v", c)
	}
	l := MustNew(WithConfig(c))
	l.Info("env file")
	l.Close()
	if b, _ := os.ReadFile(filepath.Join(dir, "svc.log")); !strings.Contains(string(b), "env file") {
		t.Errorf("%q", b)
	}
}
