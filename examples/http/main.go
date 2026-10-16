// Command http runs a small net/http server with request logging.
//
//	go run ./examples/http
//	curl -i localhost:8080/hello?name=tp
//	curl -i -H 'X-Request-ID: abc' localhost:8080/fail
package main

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/techpartners-asia/logger-go"
	"github.com/techpartners-asia/logger-go/httplog"
)

func main() {
	log := logger.MustNew(
		logger.WithFormat(logger.FormatConsole),
		logger.WithLevel(logger.LevelDebug),
		logger.WithService("example-http"),
	)
	logger.SetDefault(log)
	logger.RedirectStdLog(log, logger.LevelInfo) // http.Server internals too

	client := &http.Client{Transport: httplog.Transport(log, nil), Timeout: 5 * time.Second}

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		l := logger.FromContext(r.Context()) // carries request_id, method, path
		l.Info("saying hello", "name", r.URL.Query().Get("name"))
		_, _ = w.Write([]byte("hello " + r.URL.Query().Get("name") + "\n"))
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		logger.FromContext(r.Context()).Error("something broke", logger.Err(errors.New("db timeout")))
		http.Error(w, "nope", http.StatusInternalServerError)
	})
	mux.HandleFunc("/upstream", func(w http.ResponseWriter, r *http.Request) {
		// Outbound call: request id is forwarded, call is logged.
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://httpbin.org/status/204", nil)
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
	})
	mux.HandleFunc("/panic", func(w http.ResponseWriter, r *http.Request) { panic("oops") })
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	handler := httplog.Middleware(log, httplog.Options{
		SkipPaths:     []string{"/healthz"},
		SlowThreshold: 500 * time.Millisecond,
	})(mux)

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal("server stopped", logger.Err(err))
	}
}
