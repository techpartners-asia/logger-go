package logger

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ConsoleOptions configure NewConsoleHandler.
type ConsoleOptions struct {
	// Level is the minimum level. nil means LevelInfo.
	Level slog.Leveler
	// AddSource appends file:line to each line.
	AddSource bool
	// TimeFormat defaults to "15:04:05.000". Empty string plus NoTime hides it.
	TimeFormat string
	// UTC renders time in UTC.
	UTC bool
	// Color: nil = auto (on when w is a terminal), otherwise forced.
	Color *bool
	// ReplaceAttr is applied to every attribute (not to the built-ins).
	ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
}

// ANSI colours.
const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBlue   = "\x1b[34m"
	ansiPurple = "\x1b[35m"
	ansiCyan   = "\x1b[36m"
	ansiBgRed  = "\x1b[41;97m"
)

// ConsoleHandler renders records as readable single lines:
//
//	15:04:05.123 INF server started  port=8080 service=api
//
// Groups are flattened with dot notation (http.status=200).
type ConsoleHandler struct {
	opts   ConsoleOptions
	w      io.Writer
	mu     *sync.Mutex
	color  bool
	prefix string // rendered WithAttrs attrs
	groups []string
}

// NewConsoleHandler creates a human-friendly handler for development.
func NewConsoleHandler(w io.Writer, opts *ConsoleOptions) *ConsoleHandler {
	h := &ConsoleHandler{w: w, mu: &sync.Mutex{}}
	if opts != nil {
		h.opts = *opts
	}
	if h.opts.Level == nil {
		h.opts.Level = LevelInfo
	}
	if h.opts.TimeFormat == "" {
		h.opts.TimeFormat = defaultConsoleTimeFormat
	}
	if h.opts.Color != nil {
		h.color = *h.opts.Color
	} else {
		h.color = isTerminal(w)
	}
	return h
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (h *ConsoleHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.opts.Level.Level()
}

func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	buf := make([]byte, 0, 256)

	if !r.Time.IsZero() {
		t := r.Time
		if h.opts.UTC {
			t = t.UTC()
		}
		buf = h.paint(buf, ansiDim, t.Format(h.opts.TimeFormat))
		buf = append(buf, ' ')
	}

	buf = h.paint(buf, levelColor(r.Level), levelShort(r.Level))
	buf = append(buf, ' ')

	if r.Level >= LevelError {
		buf = h.paint(buf, ansiBold, r.Message)
	} else {
		buf = append(buf, r.Message...)
	}

	// Attributes: prefix (from WithAttrs) then record attrs, each rendered
	// with a trailing space; the stack attribute is held back and printed on
	// its own indented lines after the record.
	var stack string
	attrStart := len(buf)
	buf = append(buf, ' ', ' ')
	buf = append(buf, h.prefix...)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == KeyStack && a.Value.Kind() == slog.KindString && len(h.groups) == 0 {
			stack = a.Value.String()
			return true
		}
		buf = h.appendAttr(buf, h.groups, a)
		return true
	})
	if len(buf) == attrStart+2 {
		buf = buf[:attrStart] // no attrs at all
	} else {
		buf = bytes.TrimRight(buf, " ")
	}

	if h.opts.AddSource && r.PC != 0 {
		buf = append(buf, ' ', ' ')
		buf = h.paint(buf, ansiDim, runtimeFrames(r.PC))
	}

	if stack != "" {
		for _, line := range strings.Split(stack, "\n") {
			buf = append(buf, '\n', '\t')
			buf = h.paint(buf, ansiDim, line)
		}
	}

	buf = append(buf, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf)
	return err
}

func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	c := *h
	buf := []byte(h.prefix)
	for _, a := range attrs {
		buf = h.appendAttr(buf, h.groups, a)
	}
	c.prefix = string(buf) // keeps its trailing space as separator
	return &c
}

func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.groups = append(append([]string(nil), h.groups...), name)
	return &c
}

func (h *ConsoleHandler) appendAttr(buf []byte, groups []string, a slog.Attr) []byte {
	a.Value = a.Value.Resolve()
	if h.opts.ReplaceAttr != nil && a.Value.Kind() != slog.KindGroup {
		a = h.opts.ReplaceAttr(groups, a)
		a.Value = a.Value.Resolve()
	}
	if a.Equal(slog.Attr{}) {
		return buf
	}
	if a.Value.Kind() == slog.KindGroup {
		gs := a.Value.Group()
		if len(gs) == 0 {
			return buf
		}
		sub := groups
		if a.Key != "" {
			sub = append(append([]string(nil), groups...), a.Key)
		}
		for _, ga := range gs {
			buf = h.appendAttr(buf, sub, ga)
		}
		return buf
	}

	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}
	keyColor := ansiCyan
	valColor := ""
	if a.Key == KeyError || strings.HasSuffix(a.Key, "_error") {
		keyColor = ansiRed
		valColor = ansiRed
	}
	buf = h.paint(buf, keyColor, key)
	buf = append(buf, '=')
	buf = h.paint(buf, valColor, formatValue(a.Value))
	buf = append(buf, ' ')
	return buf
}

func (h *ConsoleHandler) paint(buf []byte, color, s string) []byte {
	if !h.color || color == "" {
		return append(buf, s...)
	}
	buf = append(buf, color...)
	buf = append(buf, s...)
	return append(buf, ansiReset...)
}

func levelColor(l slog.Level) string {
	switch {
	case l < LevelDebug:
		return ansiPurple
	case l < LevelInfo:
		return ansiBlue
	case l < LevelWarn:
		return ansiGreen
	case l < LevelError:
		return ansiYellow
	case l < LevelFatal:
		return ansiRed
	default:
		return ansiBgRed
	}
}

func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return quoteIfNeeded(v.String())
	case slog.KindTime:
		return v.Time().Format(time.RFC3339Nano)
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindAny:
		a := v.Any()
		switch x := a.(type) {
		case error:
			return quoteIfNeeded(x.Error())
		case fmt.Stringer:
			return quoteIfNeeded(x.String())
		case []byte:
			return quoteIfNeeded(string(x))
		}
		return quoteIfNeeded(fmt.Sprintf("%+v", a))
	default:
		return v.String()
	}
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if unicode.IsSpace(r) || r == '"' || r == '=' || !unicode.IsPrint(r) {
			return strconv.Quote(s)
		}
	}
	return s
}
