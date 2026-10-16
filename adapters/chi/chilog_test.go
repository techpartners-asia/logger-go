package chilog_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/techpartners-asia/logger-go"
	chilog "github.com/techpartners-asia/logger-go/adapters/chi"
	"github.com/techpartners-asia/logger-go/logtest"
)

func TestMiddleware(t *testing.T) {
	log, rec := logtest.New()
	r := chi.NewRouter()
	r.Use(chilog.Middleware(log))
	r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		chilog.FromRequest(r).Info("inside", "id", chi.URLParam(r, "id"))
		w.WriteHeader(204)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/users/42", nil))

	if in := rec.Find(logger.LevelInfo, "inside"); len(in) != 1 || in[0].String("id") != "42" || in[0].String("request_id") == "" {
		t.Errorf("%+v", in)
	}
	e := rec.Last()
	if e.String("route") != "/users/{id}" || e.String("path") != "/users/42" || e.Attrs["status"] != int64(204) {
		t.Errorf("%+v", e)
	}
}
