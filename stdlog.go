package logger

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"
)

// Writer returns an io.Writer that logs each written line at level. Useful
// for libraries that only accept an io.Writer or *log.Logger.
func (l *Logger) Writer(level Level) io.Writer {
	return &levelWriter{l: l, level: level}
}

// Std returns a *log.Logger that forwards to this logger at level.
func (l *Logger) Std(level Level) *log.Logger {
	return log.New(l.Writer(level), "", 0)
}

// RedirectStdLog routes the standard library "log" package into l at level
// and returns a function that restores the previous configuration.
func RedirectStdLog(l *Logger, level Level) (restore func()) {
	prevFlags := log.Flags()
	prevPrefix := log.Prefix()
	prevWriter := log.Writer()
	log.SetFlags(0)
	log.SetPrefix("")
	log.SetOutput(l.Writer(level))
	return func() {
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
		log.SetOutput(prevWriter)
	}
}

type levelWriter struct {
	l     *Logger
	level Level
}

func (w *levelWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		line := p
		if i := bytes.IndexByte(p, '\n'); i >= 0 {
			line = p[:i]
			p = p[i+1:]
		} else {
			p = nil
		}
		msg := strings.TrimRight(string(line), "\r")
		if msg == "" {
			continue
		}
		w.l.LogAttrs(context.Background(), w.level, msg)
	}
	return n, nil
}
