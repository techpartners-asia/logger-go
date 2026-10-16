// Package pgxlog provides query tracing for pgx v5.
//
//	cfg, _ := pgxpool.ParseConfig(dsn)
//	cfg.ConnConfig.Tracer = pgxlog.NewTracer(log, pgxlog.Config{SlowThreshold: 200 * time.Millisecond})
//	pool, _ := pgxpool.NewWithConfig(ctx, cfg)
//
// Queries log at DEBUG with sql, rows and duration_ms, slow ones at WARN,
// failures at ERROR, all with the request's context fields.
//
// The package also implements pgx's tracelog.Logger for people who already
// use tracelog.TraceLog:
//
//	cfg.ConnConfig.Tracer = &tracelog.TraceLog{Logger: pgxlog.NewLogger(log), LogLevel: tracelog.LogLevelInfo}
package pgxlog

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/techpartners-asia/logger-go"
)

// Config tunes the tracer.
type Config struct {
	// SlowThreshold logs slower queries at WARN. Default 200ms.
	SlowThreshold time.Duration
	// QueryLevel is the level for normal queries. Default DEBUG.
	QueryLevel logger.Level
	// LogArgs includes query arguments. Off by default: arguments often carry
	// personal data, and redaction cannot see inside a positional list.
	LogArgs bool
	// IgnoreNoRows treats pgx.ErrNoRows as a normal query. Default true.
	IgnoreNoRows *bool
}

type ctxKey struct{}

type queryStart struct {
	sql  string
	args []any
	t    time.Time
}

// Tracer implements pgx.QueryTracer.
type Tracer struct {
	log *logger.Logger
	cfg Config
}

// NewTracer creates a QueryTracer.
func NewTracer(log *logger.Logger, cfgs ...Config) *Tracer {
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
	if cfg.QueryLevel == 0 {
		cfg.QueryLevel = logger.LevelDebug
	}
	if cfg.IgnoreNoRows == nil {
		t := true
		cfg.IgnoreNoRows = &t
	}
	return &Tracer{log: log.Named("pgx"), cfg: cfg}
}

func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, ctxKey{}, queryStart{sql: data.SQL, args: data.Args, t: time.Now()})
}

func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	qs, ok := ctx.Value(ctxKey{}).(queryStart)
	if !ok {
		return
	}
	elapsed := time.Since(qs.t)
	attrs := []logger.Attr{
		logger.String("sql", qs.sql),
		logger.Duration(elapsed),
		logger.Int64("rows", data.CommandTag.RowsAffected()),
	}
	if t.cfg.LogArgs && len(qs.args) > 0 {
		attrs = append(attrs, logger.Any("args", qs.args))
	}
	err := data.Err
	if err != nil && *t.cfg.IgnoreNoRows && errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	switch {
	case err != nil:
		t.log.LogAttrs(ctx, logger.LevelError, "query failed", append(attrs, logger.Err(err))...)
	case elapsed > t.cfg.SlowThreshold:
		t.log.LogAttrs(ctx, logger.LevelWarn, "slow query", append(attrs, logger.Bool("slow", true))...)
	default:
		t.log.LogAttrs(ctx, t.cfg.QueryLevel, "query", attrs...)
	}
}

// Logger implements tracelog.Logger.
type Logger struct{ log *logger.Logger }

// NewLogger creates a tracelog.Logger.
func NewLogger(log *logger.Logger) *Logger {
	if log == nil {
		log = logger.Default()
	}
	return &Logger{log: log.Named("pgx")}
}

func (l *Logger) Log(ctx context.Context, level tracelog.LogLevel, msg string, data map[string]any) {
	attrs := make([]logger.Attr, 0, len(data))
	for k, v := range data {
		if k == "time" {
			if d, ok := v.(time.Duration); ok {
				attrs = append(attrs, logger.Duration(d))
				continue
			}
		}
		if k == "err" {
			if e, ok := v.(error); ok {
				attrs = append(attrs, logger.Err(e))
				continue
			}
		}
		attrs = append(attrs, logger.Any(k, v))
	}
	var lvl logger.Level
	switch level {
	case tracelog.LogLevelTrace:
		lvl = logger.LevelTrace
	case tracelog.LogLevelDebug:
		lvl = logger.LevelDebug
	case tracelog.LogLevelInfo:
		lvl = logger.LevelInfo
	case tracelog.LogLevelWarn:
		lvl = logger.LevelWarn
	case tracelog.LogLevelError:
		lvl = logger.LevelError
	default:
		lvl = logger.LevelInfo
	}
	l.log.LogAttrs(ctx, lvl, msg, attrs...)
}
