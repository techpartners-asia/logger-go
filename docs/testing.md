# Testing

## Testing with logtest

`logtest.Recorder` is an in-memory handler for asserting on log output. No
parsing JSON in tests.

```go
import "github.com/techpartners-asia/logger-go/logtest"

func TestRetry(t *testing.T) {
	log, rec := logtest.New()          // every level, nothing written anywhere
	svc := NewService(log)

	svc.Do(ctx)

	if !rec.Has(logger.LevelWarn, "retrying") {
		t.Fatalf("expected retry warning, got %v", rec.Messages())
	}
	e := rec.Find(logger.LevelWarn, "retrying")[0]
	if e.Attrs["attempt"] != int64(2) { ... }
	if v, _ := e.Get("http.status"); v != int64(503) { ... }   // groups are nested maps
	rec.Reset()
}
```

`Recorder` also satisfies `slog.Handler`, so it can be injected anywhere a
handler is accepted.
