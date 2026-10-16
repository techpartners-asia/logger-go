package echolog_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/techpartners-asia/logger-go"
	echolog "github.com/techpartners-asia/logger-go/adapters/echo"
	"github.com/techpartners-asia/logger-go/logtest"
)

func TestMiddleware(t *testing.T) {
	log, rec := logtest.New()
	e := echo.New()
	e.Use(echolog.New(log, echolog.Config{SkipPaths: []string{"/healthz"}}))
	e.GET("/items/:id", func(c echo.Context) error {
		echolog.FromCtx(c).Info("inside")
		return c.String(200, "ok")
	})
	e.GET("/nf", func(c echo.Context) error { return echo.ErrNotFound })
	e.GET("/boom", func(c echo.Context) error { return errors.New("plain") })
	e.GET("/panic", func(c echo.Context) error { panic("ep") })
	e.GET("/healthz", func(c echo.Context) error { return c.NoContent(200) })

	do := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Header.Set("X-Request-ID", "fixed")
		e.ServeHTTP(w, req)
		return w
	}

	w := do("/items/7")
	if w.Header().Get("X-Request-ID") != "fixed" {
		t.Error("request id")
	}
	if in := rec.Find(logger.LevelInfo, "inside"); len(in) != 1 || in[0].String("request_id") != "fixed" {
		t.Errorf("%+v", in)
	}
	if e := rec.Last(); e.Attrs["status"] != int64(200) || e.String("route") != "/items/:id" || e.Attrs["bytes"] != int64(2) {
		t.Errorf("%+v", e)
	}
	rec.Reset()
	if w := do("/nf"); w.Code != 404 {
		t.Errorf("code %d", w.Code)
	}
	if e := rec.Last(); e.Level != logger.LevelWarn || e.Attrs["status"] != int64(404) {
		t.Errorf("%+v", e)
	}
	rec.Reset()
	if w := do("/boom"); w.Code != 500 {
		t.Errorf("code %d", w.Code)
	}
	if e := rec.Last(); e.Level != logger.LevelError || e.String("error") != "plain" {
		t.Errorf("%+v", e)
	}
	rec.Reset()
	if w := do("/panic"); w.Code != 500 {
		t.Errorf("panic code %d", w.Code)
	}
	if !rec.Has(logger.LevelError, "http panic") {
		t.Errorf("%+v", rec.Entries())
	}
	rec.Reset()
	do("/healthz")
	if rec.Len() != 0 {
		t.Error("healthz logged")
	}
}
