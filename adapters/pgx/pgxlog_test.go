package pgxlog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/techpartners-asia/logger-go"
	pgxlog "github.com/techpartners-asia/logger-go/adapters/pgx"
	"github.com/techpartners-asia/logger-go/logtest"
)

func TestTracer(t *testing.T) {
	log, rec := logtest.New()
	tr := pgxlog.NewTracer(log, pgxlog.Config{SlowThreshold: 30 * time.Millisecond, LogArgs: true})
	base := logger.ContextWithFields(context.Background(), logger.String("request_id", "p1"))

	ctx := tr.TraceQueryStart(base, nil, pgx.TraceQueryStartData{SQL: "select $1", Args: []any{1}})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})
	e := rec.Last()
	if e.Message != "query" || e.Level != logger.LevelDebug || e.String("sql") != "select $1" || e.Attrs["rows"] != int64(1) || e.String("request_id") != "p1" {
		t.Errorf("%+v", e)
	}
	if _, ok := e.Get("args"); !ok {
		t.Error("args missing")
	}

	ctx = tr.TraceQueryStart(base, nil, pgx.TraceQueryStartData{SQL: "slow"})
	time.Sleep(40 * time.Millisecond)
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	if e := rec.Last(); e.Message != "slow query" || e.Level != logger.LevelWarn {
		t.Errorf("%+v", e)
	}

	ctx = tr.TraceQueryStart(base, nil, pgx.TraceQueryStartData{SQL: "bad"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("relation missing")})
	if e := rec.Last(); e.Message != "query failed" || e.Level != logger.LevelError {
		t.Errorf("%+v", e)
	}

	ctx = tr.TraceQueryStart(base, nil, pgx.TraceQueryStartData{SQL: "none"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: pgx.ErrNoRows})
	if e := rec.Last(); e.Message != "query" {
		t.Errorf("no rows should be normal: %+v", e)
	}
}

func TestTracelogLogger(t *testing.T) {
	log, rec := logtest.New()
	l := pgxlog.NewLogger(log)
	l.Log(context.Background(), tracelog.LogLevelWarn, "Query", map[string]any{"sql": "x", "time": 5 * time.Millisecond, "err": errors.New("e")})
	e := rec.Last()
	if e.Level != logger.LevelWarn || e.String("sql") != "x" || e.String("error") != "e" || e.String("logger") != "pgx" {
		t.Errorf("%+v", e)
	}
	if _, ok := e.Get("duration_ms"); !ok {
		t.Error("duration")
	}
}
