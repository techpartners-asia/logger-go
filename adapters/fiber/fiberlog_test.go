package fiberlog_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/techpartners-asia/logger-go"
	fiberlog "github.com/techpartners-asia/logger-go/adapters/fiber"
	"github.com/techpartners-asia/logger-go/logtest"
)

func TestMiddleware(t *testing.T) {
	log, rec := logtest.New()
	app := fiber.New()
	app.Use(fiberlog.New(log, fiberlog.Config{SkipPaths: []string{"/healthz"}}))
	app.Get("/ok", func(c fiber.Ctx) error {
		fiberlog.FromCtx(c).Info("inside")
		logger.FromContext(c.Context()).InfoContext(c.Context(), "via ctx")
		return c.Status(201).SendString("yes")
	})
	app.Get("/err", func(c fiber.Ctx) error { return fiber.ErrNotFound })
	app.Get("/boom", func(c fiber.Ctx) error { return errors.New("internal") })
	app.Get("/panic", func(c fiber.Ctx) error { panic("p") })
	app.Get("/healthz", func(c fiber.Ctx) error { return c.SendString("ok") })

	do := func(path string) *http.Response {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Request-ID", "fixed")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := do("/ok?q=1")
	if resp.StatusCode != 201 || resp.Header.Get("X-Request-ID") != "fixed" {
		t.Errorf("status=%d id=%q", resp.StatusCode, resp.Header.Get("X-Request-ID"))
	}
	in := rec.Find(logger.LevelInfo, "inside")
	if len(in) != 1 || in[0].String("request_id") != "fixed" || in[0].String("path") != "/ok" {
		t.Errorf("inside = %+v", in)
	}
	if ctxLine := rec.Find(logger.LevelInfo, "via ctx"); len(ctxLine) != 1 || ctxLine[0].String("request_id") != "fixed" {
		t.Errorf("ctx = %+v", ctxLine)
	}
	e := rec.Last()
	if e.String("request_id") != "fixed" || e.String("method") != "GET" || e.String("path") != "/ok" {
		t.Errorf("completion missing correlation: %v", e.Attrs)
	}
	if e.Message != "http request" || e.Attrs["status"] != int64(201) || e.Attrs["bytes"] != int64(3) || e.String("query") != "q=1" {
		t.Errorf("done = %+v", e)
	}

	rec.Reset()
	do("/err")
	if e := rec.Last(); e.Level != logger.LevelWarn || e.Attrs["status"] != int64(404) {
		t.Errorf("404 = %+v", e)
	}
	rec.Reset()
	do("/boom")
	if e := rec.Last(); e.Level != logger.LevelError || e.String("error") != "internal" {
		t.Errorf("500 = %+v", e)
	}
	rec.Reset()
	if r := do("/panic"); r.StatusCode != 500 {
		t.Errorf("panic status %d", r.StatusCode)
	}
	if p := rec.Find(logger.LevelError, "http panic"); len(p) != 1 || p[0].String("panic") != "p" {
		t.Errorf("panic = %+v", rec.Entries())
	}
	rec.Reset()
	do("/healthz")
	if rec.Len() != 0 {
		t.Error("healthz logged")
	}
}
