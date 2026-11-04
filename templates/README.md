# Templates

Starter projects with logging already wired in. Each folder is its own Go
module that depends on the published logger-go versions, so you can copy one
out of this repo and build it as is.

| You are building | Template | Uses |
|---|---|---|
| REST API on Fiber, with a database and outbound HTTP calls | [`fiber-api`](fiber-api/main.go) | Fiber v3, GORM (Postgres), Resty |
| REST API on Gin or plain `net/http`, with raw SQL | [`gin-api`](gin-api/main.go) | Gin, pgx pool |
| gRPC service or client | [`grpc-service`](grpc-service/main.go) | gRPC interceptors, health service |
| Queue consumer, background jobs, cron | [`worker`](worker/main.go) | core only |
| Command-line tool, script, one-off migration | [`cli`](cli/main.go) | core, `httplog.Transport` |
| SDK or shared library other services import | [`sdk-client`](sdk-client/payments.go) | Resty, `logtest` |

Using Echo, chi or gorilla/mux? Start from `gin-api` and swap the middleware
line for the one in the [adapters section](../README.md#adapters). Every HTTP
adapter writes the same fields.

## Start a new service from a template

```bash
cp -r templates/fiber-api ~/code/orders-api
cd ~/code/orders-api
go mod edit -module github.com/techpartners-asia/orders-api
go mod tidy
```

Then change the `WithService("orders-api")` name in `main.go`. Each template
has run instructions at the top of `main.go`.

## What every template sets up

All of them build the logger the same way:

```go
log := logger.MustNew(
	logger.WithService("orders-api"),
	logger.WithVersion(version),              // -ldflags "-X main.version=1.4.0"
	logger.WithRedaction(),                   // passwords, tokens, card numbers
	logger.WithStackTrace(logger.LevelError),
	logger.WithConfig(logger.ConfigFromEnv()), // LOG_* variables win over the lines above
)
defer log.Close()
logger.SetDefault(log)
```

Code sets the defaults and the environment overrides them, so one binary runs
as console output with debug logs on a laptop and as JSON in production.

The services also share these behaviors:

- The middleware writes one access line per request with the same field names
  everywhere: `request_id`, `method`, `path`, `status`, `duration_ms`.
- The request context goes into every database and HTTP call, so SQL lines
  and outbound call lines carry the same `request_id` as the access line.
  Outbound calls forward it as `X-Request-ID` or gRPC metadata.
- Not-found and validation errors log at WARN or INFO. ERROR means a person
  should look.
- SIGTERM stops the service gracefully, and `Close` flushes the log file.

## What the output looks like

`fiber-api`, one `POST /orders` (JSON, trimmed):

```json
{"level":"DEBUG","msg":"charging card","request_id":"da6f…","method":"POST","path":"/orders","card_number":"[REDACTED]"}
{"level":"DEBUG","msg":"http client request","logger":"payments","method":"POST","url":"https://httpbin.org/post","status":200,"duration_ms":850.3,"request_id":"da6f…"}
{"level":"DEBUG","msg":"query","logger":"gorm","sql":"INSERT INTO \"orders\" …","rows":1,"duration_ms":1.86,"request_id":"da6f…"}
{"level":"INFO","msg":"order created","request_id":"da6f…","order_id":1}
{"level":"WARN","msg":"http request","status":201,"duration_ms":853.3,"tenant":"acme","slow":true,"request_id":"da6f…"}
```

`worker` (console):

```
13:16:27.607 ERR job panicked  logger=consumer worker=1 panic="nil inventory record" job_id=job-0006 job_kind=send_receipt
	main.handle
		worker/main.go:136
13:16:28.307 WRN job retrying  logger=consumer worker=2 error="upstream unavailable" attempt=1 job_id=job-0008
13:16:29.181 INF job done  logger=consumer worker=2 attempt=3 duration_ms=1086.9 job_id=job-0008
13:16:30.988 INF cron task done  logger=cron deleted=1 run=1
```

`grpc-service`, a client call followed by the server's line with the same ID:

```
INF grpc request  grpc_service=grpc.health.v1.Health grpc_method=Check grpc_code=OK duration_ms=0.013 request_id=cli-check-1
```

## Other places the logger fits

The core module has no dependencies, so it works anywhere Go runs.

| Project | What to use |
|---|---|
| Kafka, NATS, RabbitMQ or SQS consumer | `worker`. Put the message key or ID in `logger.ContextWith`. |
| Scheduled job (Kubernetes CronJob, systemd timer) | `cli`, with `LOG_FORMAT=json` and `LOG_DIR` so the output reaches Loki. |
| Database migration runner | `cli`. GORM and pgx adapters log each statement at DEBUG. |
| Webhook receiver | `gin-api` or `fiber-api`. Keep `WithRedaction()` on, because webhook bodies often carry secrets. |
| API gateway or reverse proxy | `httplog.Middleware` around `httputil.ReverseProxy`, with `httplog.Transport` on the outbound side. |
| AWS Lambda or Cloud Run | Default stderr output in JSON. The platform collects it, so `LOG_DIR` is not needed. |
| Code already on `log/slog` | `log.Slog()` or `logger.FromSlog`. Nothing else changes. |
| Third-party library that wants a `*log.Logger` or `io.Writer` | `log.Std(logger.LevelInfo)` or `log.Writer(logger.LevelWarn)`. |
| Tests that check what was logged | `logtest.New()`. See `sdk-client/payments_test.go`. |

## Deploying

On a server with Alloy and Loki, follow the
[Loki section](../README.md#production-ship-logs-to-loki). In short:

```dockerfile
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /app .

FROM gcr.io/distroless/static
COPY --from=build /app /app
ENTRYPOINT ["/app"]
```

```yaml
services:
  orders-api:
    image: orders-api:1.4.0
    environment:
      LOG_FORMAT: json
      LOG_SERVICE: orders-api
      LOG_ENV: prod
      LOG_DIR: /var/log/apps/shop        # /var/log/apps/<project>
      LOG_LEVEL: info
      LOG_SAMPLING_INITIAL: 100
      LOG_SAMPLING_THEREAFTER: 100
      DATABASE_URL: postgres://…
    volumes:
      - /var/log/apps/shop:/var/log/apps/shop
```

For local development:

```bash
LOG_FORMAT=console LOG_LEVEL=debug LOG_SOURCE=true go run .
```
