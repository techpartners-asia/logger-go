package muxlog_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/techpartners-asia/logger-go"
	muxlog "github.com/techpartners-asia/logger-go/adapters/mux"
	"github.com/techpartners-asia/logger-go/logtest"
)

func TestMiddleware(t *testing.T) {
	log, rec := logtest.New()
	r := mux.NewRouter()
	r.Use(muxlog.Middleware(log))
	r.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		muxlog.FromRequest(r).Info("inside", "id", mux.Vars(r)["id"])
		w.WriteHeader(202)
	}).Name("getUser")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/users/9", nil))

	if in := rec.Find(logger.LevelInfo, "inside"); len(in) != 1 || in[0].String("id") != "9" {
		t.Errorf("%+v", in)
	}
	e := rec.Last()
	if e.String("route") != "/users/{id}" || e.String("route_name") != "getUser" || e.Attrs["status"] != int64(202) {
		t.Errorf("%+v", e)
	}
}
