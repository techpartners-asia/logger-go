package ginlog_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/techpartners-asia/logger-go"
	ginlog "github.com/techpartners-asia/logger-go/adapters/gin"
	"github.com/techpartners-asia/logger-go/logtest"
)

func TestMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log, rec := logtest.New()
	r := gin.New()
	r.Use(ginlog.New(log, ginlog.Config{SkipPaths: []string{"/healthz"}}), ginlog.Recovery(log))
	r.GET("/items/:id", func(c *gin.Context) {
		ginlog.FromCtx(c).Info("inside")
		c.String(200, "ok")
	})
	r.GET("/bad", func(c *gin.Context) { c.Status(503) })
	r.GET("/panic", func(c *gin.Context) { panic("gp") })
	r.GET("/healthz", func(c *gin.Context) { c.Status(200) })

	do := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		r.ServeHTTP(w, req)
		return w
	}

	w := do("/items/9")
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("no request id")
	}
	if in := rec.Find(logger.LevelInfo, "inside"); len(in) != 1 || in[0].String("path") != "/items/9" {
		t.Errorf("%+v", in)
	}
	if e := rec.Last(); e.Attrs["status"] != int64(200) || e.String("route") != "/items/:id" || e.Attrs["bytes"] != int64(2) {
		t.Errorf("%+v", e)
	}
	rec.Reset()
	do("/bad")
	if e := rec.Last(); e.Level != logger.LevelError {
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
