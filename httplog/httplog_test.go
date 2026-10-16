package httplog_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/techpartners-asia/logger-go"
	"github.com/techpartners-asia/logger-go/httplog"
	"github.com/techpartners-asia/logger-go/logtest"
)

func TestMiddleware(t *testing.T) {
	log, rec := logtest.New()
	var seenID string
	h := httplog.Middleware(log, httplog.Options{SkipPaths: []string{"/healthz", "/metrics*"}, SlowThreshold: time.Hour})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenID = httplog.RequestID(r)
			logger.FromContext(r.Context()).Info("inside")
			logger.FromContext(r.Context()).InfoContext(r.Context(), "default logger with ctx")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("ok!"))
		}))

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/orders?x=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("X-Request-ID") == "" || resp.Header.Get("X-Request-ID") != seenID {
		t.Errorf("request id header=%q seen=%q", resp.Header.Get("X-Request-ID"), seenID)
	}

	inside := rec.Find(logger.LevelInfo, "inside")
	if len(inside) != 1 || inside[0].String("request_id") != seenID || inside[0].String("path") != "/orders" {
		t.Errorf("inside = %+v", inside)
	}
	done := rec.Find(logger.LevelInfo, "http request")
	if len(done) != 1 {
		t.Fatalf("done = %+v", rec.Entries())
	}
	e := done[0]
	if e.String("request_id") != seenID || e.String("path") != "/orders" {
		t.Errorf("completion missing correlation: %v", e.Attrs)
	}
	if e.Attrs["status"] != int64(201) || e.Attrs["bytes"] != int64(3) || e.String("query") != "x=1" || e.String("method") != "GET" {
		t.Errorf("attrs = %v", e.Attrs)
	}
	if _, ok := e.Get("duration_ms"); !ok {
		t.Error("duration missing")
	}

	rec.Reset()
	for _, p := range []string{"/healthz", "/metrics/foo"} {
		r, _ := http.Get(srv.URL + p)
		r.Body.Close()
	}
	if rec.Len() != 0 {
		t.Errorf("skipped paths logged: %v", rec.Messages())
	}
}

func TestMiddlewareLevelsAndPanic(t *testing.T) {
	log, rec := logtest.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("/500", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	mux.HandleFunc("/panic", func(w http.ResponseWriter, r *http.Request) { panic("kaboom") })
	srv := httptest.NewServer(httplog.Middleware(log)(mux))
	defer srv.Close()

	for _, p := range []string{"/404", "/500", "/panic"} {
		r, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if p == "/panic" && r.StatusCode != 500 {
			t.Errorf("panic status = %d", r.StatusCode)
		}
	}
	if !rec.Has(logger.LevelWarn, "http request") || !rec.Has(logger.LevelError, "http request") {
		t.Errorf("levels: %+v", rec.Entries())
	}
	p := rec.Find(logger.LevelError, "http panic")
	if len(p) != 1 || p[0].String("panic") != "kaboom" || !strings.Contains(p[0].String("stack"), "goroutine") {
		t.Errorf("panic entry = %+v", p)
	}
}

func TestTransport(t *testing.T) {
	log, rec := logtest.New()
	var gotID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.Header.Get("X-Request-ID")
		w.WriteHeader(502)
	}))
	defer upstream.Close()

	client := &http.Client{Transport: httplog.Transport(log, nil)}
	req, _ := http.NewRequest("GET", upstream.URL+"/u?token=secret", nil)
	req = req.WithContext(logger.ContextWithFields(req.Context(), logger.String(logger.KeyRequestID, "rid-1")))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotID != "rid-1" {
		t.Errorf("propagated id = %q", gotID)
	}
	e := rec.Last()
	if e.Level != logger.LevelError || e.Attrs["status"] != int64(502) || !strings.HasSuffix(e.String("url"), "/u?token=secret") {
		t.Errorf("entry = %+v", e)
	}
}
