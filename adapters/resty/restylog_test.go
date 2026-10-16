package restylog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/techpartners-asia/logger-go"
	restylog "github.com/techpartners-asia/logger-go/adapters/resty"
	"github.com/techpartners-asia/logger-go/logtest"
	"resty.dev/v3"
)

func TestInstall(t *testing.T) {
	log, rec := logtest.New()
	var gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.Header.Get("X-Request-ID")
		if r.URL.Path == "/fail" {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := restylog.Install(resty.New(), log, restylog.Config{LogResponseBody: true})
	defer client.Close()
	ctx := logger.ContextWithFields(context.Background(), logger.String("request_id", "rr-1"))

	res, err := client.R().SetContext(ctx).Get(srv.URL + "/ok")
	if err != nil || res.StatusCode() != 200 {
		t.Fatal(err, res.StatusCode())
	}
	if gotID != "rr-1" {
		t.Errorf("propagated id = %q", gotID)
	}
	e := rec.Last()
	if e.Level != logger.LevelDebug || e.Attrs["status"] != int64(200) || e.String("request_id") != "rr-1" || e.String("response_body") != `{"ok":true}` || e.String("logger") != "resty" {
		t.Errorf("%+v", e)
	}
	if _, ok := e.Get("duration_ms"); !ok {
		t.Error("duration")
	}

	_, _ = client.R().SetContext(ctx).Get(srv.URL + "/fail")
	if e := rec.Last(); e.Level != logger.LevelError || e.Attrs["status"] != int64(503) {
		t.Errorf("%+v", e)
	}

	_, err = client.R().SetContext(ctx).Get("http://127.0.0.1:1/unreachable")
	if err == nil {
		t.Fatal("expected error")
	}
	if e := rec.Last(); e.Level != logger.LevelError || e.String("error") == "" {
		t.Errorf("%+v", e)
	}
}
