package logger

import (
	"fmt"
	"log/slog"
	"strings"
)

// Level is an alias of slog.Level so callers rarely need to import log/slog.
type Level = slog.Level

// Log levels. Trace and Fatal extend the standard slog set.
const (
	LevelTrace Level = slog.Level(-8)
	LevelDebug Level = slog.LevelDebug
	LevelInfo  Level = slog.LevelInfo
	LevelWarn  Level = slog.LevelWarn
	LevelError Level = slog.LevelError
	LevelFatal Level = slog.Level(12)
	LevelPanic Level = slog.Level(16)
)

// ParseLevel converts a case-insensitive level name ("trace", "debug", "info",
// "warn"/"warning", "error", "fatal", "panic") or a numeric slog level into a
// Level.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace", "trc":
		return LevelTrace, nil
	case "debug", "dbg":
		return LevelDebug, nil
	case "info", "inf", "":
		return LevelInfo, nil
	case "warn", "warning", "wrn":
		return LevelWarn, nil
	case "error", "err":
		return LevelError, nil
	case "fatal", "ftl":
		return LevelFatal, nil
	case "panic", "pnc":
		return LevelPanic, nil
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return LevelInfo, fmt.Errorf("logger: unknown level %q", s)
	}
	return l, nil
}

// LevelName returns the canonical upper-case name of a level, including the
// extended TRACE, FATAL and PANIC levels.
func LevelName(l Level) string {
	switch {
	case l < LevelDebug:
		return "TRACE"
	case l < LevelInfo:
		return "DEBUG"
	case l < LevelWarn:
		return "INFO"
	case l < LevelError:
		return "WARN"
	case l < LevelFatal:
		return "ERROR"
	case l < LevelPanic:
		return "FATAL"
	default:
		return "PANIC"
	}
}

// levelShort returns the 3-letter console abbreviation.
func levelShort(l Level) string {
	switch {
	case l < LevelDebug:
		return "TRC"
	case l < LevelInfo:
		return "DBG"
	case l < LevelWarn:
		return "INF"
	case l < LevelError:
		return "WRN"
	case l < LevelFatal:
		return "ERR"
	case l < LevelPanic:
		return "FTL"
	default:
		return "PNC"
	}
}

// replaceLevelName is a ReplaceAttr step that rewrites the level attribute so
// extended levels print as TRACE/FATAL/PANIC instead of DEBUG-4 / ERROR+4.
func replaceLevelName(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.LevelKey {
		if lvl, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(LevelName(lvl))
		}
	}
	return a
}
