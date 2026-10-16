// Package gormlog implements GORM's logger.Interface on top of the
// techpartners logger.
//
//	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
//		Logger: gormlog.New(log, gormlog.Config{SlowThreshold: 200 * time.Millisecond}),
//	})
//
// Every query is logged at DEBUG with sql, rows and duration_ms; slow
// queries at WARN; errors at ERROR. Request IDs and other context fields are
// included because GORM passes the request context through.
package gormlog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/techpartners-asia/logger-go"
	gormlogger "gorm.io/gorm/logger"
)

// Config tunes the adapter.
type Config struct {
	// SlowThreshold logs slower queries at WARN. Default 200ms.
	SlowThreshold time.Duration
	// IgnoreRecordNotFound skips logging gorm.ErrRecordNotFound as an error.
	// Default true.
	IgnoreRecordNotFound *bool
	// LogLevel is GORM's level filter. Default Info (log everything;
	// the techpartners logger level still applies on top).
	LogLevel gormlogger.LogLevel
	// QueryLevel is the level for normal queries. Default DEBUG.
	QueryLevel logger.Level
	// ParameterizedQueries hides bound values (GORM then logs "?" placeholders).
	ParameterizedQueries bool
}

// Logger implements gormlogger.Interface.
type Logger struct {
	log *logger.Logger
	cfg Config
}

// New creates a GORM logger.
func New(log *logger.Logger, cfgs ...Config) *Logger {
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if log == nil {
		log = logger.Default()
	}
	if cfg.SlowThreshold == 0 {
		cfg.SlowThreshold = 200 * time.Millisecond
	}
	if cfg.IgnoreRecordNotFound == nil {
		t := true
		cfg.IgnoreRecordNotFound = &t
	}
	if cfg.LogLevel == 0 {
		cfg.LogLevel = gormlogger.Info
	}
	if cfg.QueryLevel == 0 {
		cfg.QueryLevel = logger.LevelDebug
	}
	return &Logger{log: log.Named("gorm"), cfg: cfg}
}

// LogMode returns a copy with a different GORM level.
func (l *Logger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	c := *l
	c.cfg.LogLevel = level
	return &c
}

// ParamsFilter implements gormlogger.ParamsFilter.
func (l *Logger) ParamsFilter(_ context.Context, sql string, params ...any) (string, []any) {
	if l.cfg.ParameterizedQueries {
		return sql, nil
	}
	return sql, params
}

func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	if l.cfg.LogLevel >= gormlogger.Info {
		l.log.InfoContext(ctx, fmt.Sprintf(msg, args...))
	}
}

func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	if l.cfg.LogLevel >= gormlogger.Warn {
		l.log.WarnContext(ctx, fmt.Sprintf(msg, args...))
	}
}

func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	if l.cfg.LogLevel >= gormlogger.Error {
		l.log.ErrorContext(ctx, fmt.Sprintf(msg, args...))
	}
}

// Trace logs a finished query.
func (l *Logger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.cfg.LogLevel <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	notFound := errors.Is(err, gormlogger.ErrRecordNotFound)

	switch {
	case err != nil && l.cfg.LogLevel >= gormlogger.Error && !(notFound && *l.cfg.IgnoreRecordNotFound):
		sql, rows := fc()
		l.log.LogAttrs(ctx, logger.LevelError, "query failed", l.attrs(sql, rows, elapsed, err)...)
	case elapsed > l.cfg.SlowThreshold && l.cfg.LogLevel >= gormlogger.Warn:
		sql, rows := fc()
		attrs := append(l.attrs(sql, rows, elapsed, nil), logger.Bool("slow", true),
			logger.String("slow_threshold", l.cfg.SlowThreshold.String()))
		l.log.LogAttrs(ctx, logger.LevelWarn, "slow query", attrs...)
	case l.cfg.LogLevel >= gormlogger.Info:
		if !l.log.Enabled(ctx, l.cfg.QueryLevel) {
			return
		}
		sql, rows := fc()
		l.log.LogAttrs(ctx, l.cfg.QueryLevel, "query", l.attrs(sql, rows, elapsed, nil)...)
	}
}

func (l *Logger) attrs(sql string, rows int64, d time.Duration, err error) []logger.Attr {
	attrs := []logger.Attr{logger.String("sql", sql), logger.Duration(d)}
	if rows >= 0 {
		attrs = append(attrs, logger.Int64("rows", rows))
	}
	if err != nil {
		attrs = append(attrs, logger.Err(err))
	}
	return attrs
}
