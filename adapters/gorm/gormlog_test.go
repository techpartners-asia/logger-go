package gormlog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/techpartners-asia/logger-go"
	gormlog "github.com/techpartners-asia/logger-go/adapters/gorm"
	"github.com/techpartners-asia/logger-go/logtest"
	gormlogger "gorm.io/gorm/logger"
)

func TestTrace(t *testing.T) {
	log, rec := logtest.New()
	gl := gormlog.New(log, gormlog.Config{SlowThreshold: 50 * time.Millisecond})
	ctx := logger.ContextWithFields(context.Background(), logger.String("request_id", "r9"))
	fc := func() (string, int64) { return "SELECT 1", 1 }

	gl.Trace(ctx, time.Now(), fc, nil)
	if e := rec.Last(); e.Message != "query" || e.Level != logger.LevelDebug || e.String("sql") != "SELECT 1" || e.String("request_id") != "r9" || e.String("logger") != "gorm" {
		t.Errorf("%+v", e)
	}
	gl.Trace(ctx, time.Now().Add(-time.Second), fc, nil)
	if e := rec.Last(); e.Message != "slow query" || e.Level != logger.LevelWarn || e.Attrs["slow"] != true {
		t.Errorf("%+v", e)
	}
	gl.Trace(ctx, time.Now(), fc, errors.New("syntax error"))
	if e := rec.Last(); e.Message != "query failed" || e.Level != logger.LevelError || e.String("error") != "syntax error" {
		t.Errorf("%+v", e)
	}
	rec.Reset()
	gl.Trace(ctx, time.Now(), fc, gormlogger.ErrRecordNotFound)
	if e := rec.Last(); e.Message != "query" {
		t.Errorf("not-found should be a normal query: %+v", e)
	}
	rec.Reset()
	gl.LogMode(gormlogger.Silent).Trace(ctx, time.Now(), fc, errors.New("x"))
	if rec.Len() != 0 {
		t.Error("silent logged")
	}
	gl.Warn(ctx, "cfg %s", "warn")
	if e := rec.Last(); e.Message != "cfg warn" || e.Level != logger.LevelWarn {
		t.Errorf("%+v", e)
	}
}
