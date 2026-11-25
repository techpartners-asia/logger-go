# logger-go

Structured logging SDK for techpartners.asia Go backends.

Built on the standard library's `log/slog`, so it works with every library that
speaks slog, and adds what production services actually need: JSON in prod and
a readable console in dev, service metadata on every line, request and trace
IDs that follow `context.Context`, redaction of secrets, sampling, stack traces,
runtime level switching, and drop-in middleware for net/http, Fiber, Gin and
gRPC.

```
go get github.com/techpartners-asia/logger-go
```

Zero dependencies in the core module. Framework adapters live in their own
modules under `adapters/` so importing the logger never drags in Fiber or gRPC.

---

## Documentation

| Guide | Covers |
|---|---|
| [Configuration](docs/configuration.md) | Output formats, options, environment variables, `Config` struct |
| [Logging](docs/logging.md) | Logging API, context propagation, interop with slog and std log |
| [Handlers](docs/handlers.md) | Redaction, sampling, stack traces, multiple outputs |
| [Production](docs/production.md) | Rotating log files and shipping logs to Loki |
| [HTTP middleware and adapters](docs/adapters.md) | net/http, Fiber, Gin, Echo, chi, mux, gRPC, Resty, GORM, pgx, OpenTelemetry |
| [Testing](docs/testing.md) | Asserting on logs with `logtest` |
| [Conventions](docs/conventions.md) | Service conventions and migrating from zap, zerolog, logrus |
| [Logging policy](docs/logging-policy.md) | What the company procedure says to log, never log and retain, and how to do it here |
| [Architecture](docs/architecture.md) | How the handler chain is built, and benchmarks |
| [Development](docs/development.md) | Building, testing and releasing this repo |

---

## Quick start

```go
package main

import (
	"errors"

	"github.com/techpartners-asia/logger-go"
)

func main() {
	log := logger.MustNew(
		logger.WithConfig(logger.ConfigFromEnv()), // LOG_LEVEL, LOG_FORMAT, ...
		logger.WithService("payments-api"),
		logger.WithVersion(Version),
		logger.WithRedaction(),
	)
	defer log.Close()
	logger.SetDefault(log) // also becomes slog.Default()

	log.Info("server started", "port", 8080)
	log.With("user_id", 42).Warn("quota almost reached", "used", 0.95)
	log.Error("charge failed", errors.New("card declined"), "order_id", 991)
}
```

Production (`LOG_FORMAT=json`, the default):

```json
{"time":"2026-10-16T12:24:24.743+08:00","level":"INFO","msg":"server started","service":"payments-api","version":"1.4.0","port":8080}
{"time":"2026-10-16T12:24:24.745+08:00","level":"WARN","msg":"quota almost reached","service":"payments-api","version":"1.4.0","user_id":42,"used":0.95}
{"time":"2026-10-16T12:24:24.745+08:00","level":"ERROR","msg":"charge failed","service":"payments-api","version":"1.4.0","error":"card declined","order_id":991}
```

Development (`LOG_FORMAT=console`), colored when stderr is a terminal:

```
12:24:46.489 INF server started  service=payments-api version=1.4.0 port=8080
12:24:46.489 WRN quota almost reached  service=payments-api version=1.4.0 user_id=42 used=0.95
12:24:46.489 ERR charge failed  service=payments-api version=1.4.0 error="card declined" order_id=991
```

Try it: `go run ./examples/basic` and `LOG_FORMAT=console LOG_LEVEL=trace go run ./examples/basic`.

---

## Templates: start a new service

[`templates/`](templates/README.md) has starter projects with logging already
wired in. Copy one and rename it:

| You are building | Template |
|---|---|
| REST API on Fiber with GORM and outbound HTTP | [`fiber-api`](templates/fiber-api/main.go) |
| REST API on Gin or `net/http` with pgx | [`gin-api`](templates/gin-api/main.go) |
| gRPC service or client | [`grpc-service`](templates/grpc-service/main.go) |
| Queue consumer, background jobs, cron | [`worker`](templates/worker/main.go) |
| Command-line tool or one-off script | [`cli`](templates/cli/main.go) |
| SDK or shared library | [`sdk-client`](templates/sdk-client/payments.go) |

The [templates README](templates/README.md) also covers other project types
(Kafka consumers, Lambda, migrations, gateways), sample output, and a
Dockerfile and compose file that ship logs to Loki.

## License

MIT. See [LICENSE](LICENSE).
