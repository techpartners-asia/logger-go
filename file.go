package logger

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileConfig describes a log file with rotation. Zero values are sensible
// defaults; only Dir is really needed.
//
//	logger.WithFile(logger.FileConfig{Dir: "/var/log/orders"})
//
// writes /var/log/orders/<service>.log, rotates at 100 MB, keeps 10 backups
// for 30 days.
type FileConfig struct {
	// Dir is created if missing. Default "./logs".
	Dir string `json:"dir" yaml:"dir" env:"LOG_DIR"`
	// Filename inside Dir. Default "<service>.log", or "app.log" when no
	// service name is configured.
	Filename string `json:"filename" yaml:"filename" env:"LOG_FILE"`
	// MaxSizeMB rotates the file when it exceeds this size. Default 100.
	// Negative disables size rotation.
	MaxSizeMB int `json:"max_size_mb" yaml:"max_size_mb" env:"LOG_FILE_MAX_SIZE_MB"`
	// MaxBackups is the number of rotated files to keep. Default 10.
	// Negative keeps all.
	MaxBackups int `json:"max_backups" yaml:"max_backups" env:"LOG_FILE_MAX_BACKUPS"`
	// MaxAgeDays removes rotated files older than this. Default 30.
	// Negative keeps all.
	MaxAgeDays int `json:"max_age_days" yaml:"max_age_days" env:"LOG_FILE_MAX_AGE_DAYS"`
	// Daily also rotates at local midnight.
	Daily bool `json:"daily" yaml:"daily" env:"LOG_FILE_DAILY"`
	// Compress gzips rotated files in the background.
	Compress bool `json:"compress" yaml:"compress" env:"LOG_FILE_COMPRESS"`
	// Tee also writes every record to stderr (same format). Handy for
	// containers that ship stdout/stderr while ops also want a local file.
	Tee bool `json:"tee" yaml:"tee" env:"LOG_FILE_TEE"`
	// Perm for created files. Default 0644. Directories get 0755.
	Perm os.FileMode `json:"-" yaml:"-"`
}

func (c FileConfig) withDefaults(service string) FileConfig {
	if c.Dir == "" {
		c.Dir = "logs"
	}
	if c.Filename == "" {
		if service != "" {
			c.Filename = sanitizeFilename(service) + ".log"
		} else {
			c.Filename = "app.log"
		}
	}
	if c.MaxSizeMB == 0 {
		c.MaxSizeMB = 100
	}
	if c.MaxBackups == 0 {
		c.MaxBackups = 10
	}
	if c.MaxAgeDays == 0 {
		c.MaxAgeDays = 30
	}
	if c.Perm == 0 {
		c.Perm = 0o644
	}
	return c
}

func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, s)
}

// WithDir writes logs to a rotating file in dir, named after the service.
// Shorthand for WithFile(FileConfig{Dir: dir}).
func WithDir(dir string) Option { return WithFile(FileConfig{Dir: dir}) }

// WithFile writes logs to a rotating file. It replaces the output unless
// cfg.Tee is set, in which case stderr receives a copy. Close flushes and
// closes the file.
func WithFile(cfg FileConfig) Option { return func(o *options) { o.file = &cfg } }

// RotatingWriter is a size- and time-based rotating file writer. It is safe
// for concurrent use and can be used on its own with any logger.
type RotatingWriter struct {
	cfg FileConfig

	mu       sync.Mutex
	f        *os.File
	size     int64
	openDay  int // yyyymmdd of the current file, for Daily
	closed   bool
	bgWG     sync.WaitGroup
	maxBytes int64
}

// NewRotatingWriter opens (or creates) the log file described by cfg.
func NewRotatingWriter(cfg FileConfig) (*RotatingWriter, error) {
	cfg = cfg.withDefaults("")
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("logger: create log dir: %w", err)
	}
	w := &RotatingWriter{cfg: cfg, maxBytes: int64(cfg.MaxSizeMB) * 1024 * 1024}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// Path returns the active log file path.
func (w *RotatingWriter) Path() string { return filepath.Join(w.cfg.Dir, w.cfg.Filename) }

func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.Path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, w.cfg.Perm)
	if err != nil {
		return fmt.Errorf("logger: open log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("logger: stat log file: %w", err)
	}
	w.f = f
	w.size = info.Size()
	day := dayOf(time.Now())
	if info.Size() > 0 {
		day = dayOf(info.ModTime())
	}
	w.openDay = day
	return nil
}

func dayOf(t time.Time) int { return t.Year()*10000 + int(t.Month())*100 + t.Day() }

// Write implements io.Writer, rotating first when needed.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, errors.New("logger: write to closed file")
	}
	now := time.Now()
	needRotate := (w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes && w.size > 0) ||
		(w.cfg.Daily && dayOf(now) != w.openDay)
	if needRotate {
		if err := w.rotateLocked(now); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// Rotate forces a rotation now.
func (w *RotatingWriter) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rotateLocked(time.Now())
}

func (w *RotatingWriter) rotateLocked(now time.Time) error {
	if err := w.f.Close(); err != nil {
		return err
	}
	backup := w.backupName(now)
	if err := os.Rename(w.Path(), backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("logger: rotate: %w", err)
	}
	if err := w.open(); err != nil {
		return err
	}
	w.openDay = dayOf(now)
	w.bgWG.Add(1)
	go func() {
		defer w.bgWG.Done()
		if w.cfg.Compress {
			_ = compressFile(backup)
		}
		w.cleanup()
	}()
	return nil
}

func (w *RotatingWriter) backupName(now time.Time) string {
	ext := filepath.Ext(w.cfg.Filename)
	base := strings.TrimSuffix(w.cfg.Filename, ext)
	name := fmt.Sprintf("%s-%s%s", base, now.Format("20060102T150405.000"), ext)
	p := filepath.Join(w.cfg.Dir, name)
	for i := 1; ; i++ {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			if _, err := os.Stat(p + ".gz"); errors.Is(err, os.ErrNotExist) {
				return p
			}
		}
		p = filepath.Join(w.cfg.Dir, fmt.Sprintf("%s-%s.%d%s", base, now.Format("20060102T150405.000"), i, ext))
	}
}

type backupFile struct {
	path string
	mod  time.Time
}

// backups lists rotated files for this log, newest first.
func (w *RotatingWriter) backups() []backupFile {
	ext := filepath.Ext(w.cfg.Filename)
	base := strings.TrimSuffix(w.cfg.Filename, ext)
	entries, err := os.ReadDir(w.cfg.Dir)
	if err != nil {
		return nil
	}
	var out []backupFile
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == w.cfg.Filename || !strings.HasPrefix(n, base+"-") {
			continue
		}
		if !(strings.HasSuffix(n, ext) || strings.HasSuffix(n, ext+".gz")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, backupFile{path: filepath.Join(w.cfg.Dir, n), mod: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	return out
}

func (w *RotatingWriter) cleanup() {
	files := w.backups()
	var remove []string
	if w.cfg.MaxBackups > 0 && len(files) > w.cfg.MaxBackups {
		for _, f := range files[w.cfg.MaxBackups:] {
			remove = append(remove, f.path)
		}
		files = files[:w.cfg.MaxBackups]
	}
	if w.cfg.MaxAgeDays > 0 {
		cutoff := time.Now().Add(-time.Duration(w.cfg.MaxAgeDays) * 24 * time.Hour)
		for _, f := range files {
			if f.mod.Before(cutoff) {
				remove = append(remove, f.path)
			}
		}
	}
	for _, p := range remove {
		_ = os.Remove(p)
	}
}

func compressFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	dst, err := os.OpenFile(path+".gz", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(dst)
	if _, err := io.Copy(gz, src); err != nil {
		gz.Close()
		dst.Close()
		os.Remove(path + ".gz")
		return err
	}
	if err := gz.Close(); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	_ = os.Chtimes(path+".gz", info.ModTime(), info.ModTime())
	return os.Remove(path)
}

// Sync flushes the file to disk.
func (w *RotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	return w.f.Sync()
}

// Close waits for background compression/cleanup and closes the file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	err := w.f.Close()
	w.mu.Unlock()
	w.bgWG.Wait()
	return err
}

// teeWriter writes to a file and a secondary writer, syncing/closing only
// the file.
type teeWriter struct {
	file  *RotatingWriter
	other io.Writer
}

func (t *teeWriter) Write(p []byte) (int, error) {
	n, err := t.file.Write(p)
	_, _ = t.other.Write(p)
	return n, err
}

func (t *teeWriter) Sync() error  { return t.file.Sync() }
func (t *teeWriter) Close() error { return t.file.Close() }
