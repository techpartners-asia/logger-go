// Command cli is a starter command-line tool. Results go to stdout, logs go to
// stderr, so `cli ... > out.txt` keeps the output clean.
//
//	go run . https://example.com https://httpbin.org/status/503
//	go run . -v https://example.com          # debug logs
//	go run . -json https://example.com       # JSON logs for CI
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/techpartners-asia/logger-go"
	"github.com/techpartners-asia/logger-go/httplog"
)

func main() {
	verbose := flag.Bool("v", false, "debug logging")
	asJSON := flag.Bool("json", false, "JSON logs instead of console")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request timeout")
	flag.Parse()

	format := logger.FormatConsole
	if *asJSON {
		format = logger.FormatJSON
	}
	level := logger.LevelInfo
	if *verbose {
		level = logger.LevelDebug
	}
	log := logger.MustNew(logger.WithFormat(format), logger.WithLevel(level))
	defer log.Close()

	if flag.NArg() == 0 {
		log.Fatal("no URLs given", "usage", "cli [-v] [-json] URL...")
	}

	// The transport logs each call at DEBUG, so -v shows request details.
	client := &http.Client{Transport: httplog.Transport(log, nil), Timeout: *timeout}

	failed := 0
	for _, url := range flag.Args() {
		l := log.With("url", url)
		start := time.Now()
		status, err := check(context.Background(), client, url)
		if err != nil {
			l.Error("check failed", err)
			failed++
			continue
		}
		l.Debug("checked", "status", status, logger.Duration(time.Since(start)))
		fmt.Printf("%d\t%s\n", status, url)
		if status >= 500 {
			failed++
		}
	}

	if failed > 0 {
		log.Error("some checks failed", "failed", failed, "total", flag.NArg())
		log.Close()
		os.Exit(1)
	}
}

func check(ctx context.Context, client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
