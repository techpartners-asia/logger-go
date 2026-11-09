# HTTP middleware and adapters

## HTTP middleware (net/http)

Package `httplog` ships in the core module.

```go
import "github.com/techpartners-asia/logger-go/httplog"

handler := httplog.Middleware(log, httplog.Options{
	SkipPaths:         []string{"/healthz", "/metrics*"},
	SlowThreshold:     500 * time.Millisecond,     // slower requests logged at WARN with slow=true
	TrustProxyHeaders: true,                        // X-Forwarded-For for remote_addr
	LogRequestHeaders: false,
	Fields: func(r *http.Request, status int, d time.Duration) []logger.Attr {
		return []logger.Attr{logger.String("tenant", r.Header.Get("X-Tenant"))}
	},
})(mux)
```

Per request it:

- reads or generates `X-Request-ID` and echoes it in the response;
- stores a logger with `request_id`, `method`, `path` in the context
  (`logger.FromContext(r.Context())`) and the ID as a context field
  (`httplog.RequestID(r)`);
- logs one line on completion at INFO (2xx/3xx), WARN (4xx or slow) or ERROR
  (5xx) with `status`, `duration_ms`, `bytes`, `remote_addr`, `user_agent`,
  `query`;
- recovers panics, logs them with a stack as `http panic`, and returns 500.

The response wrapper preserves `http.Flusher` and `http.Hijacker`, so
streaming and WebSocket upgrades keep working.

Outbound calls:

```go
client := &http.Client{Transport: httplog.Transport(log, nil)}
```

logs `method`, `url`, `status`, `duration_ms` at DEBUG (WARN/ERROR on 4xx/5xx
or transport error) and forwards the incoming request ID as `X-Request-ID`.
Set `TransportOptions{Level: logger.LevelInfo}` to see successes in production.

## Adapters

Each adapter is a separate Go module. Import only what you use.

| Kind | Adapter | Import path (all under `github.com/techpartners-asia/logger-go/`) |
|---|---|---|
| HTTP server | net/http, std mux, httprouter, ... | `httplog` (in core) |
| HTTP server | Fiber v3 | `adapters/fiber` |
| HTTP server | Gin | `adapters/gin` |
| HTTP server | Echo v4 | `adapters/echo` |
| HTTP server | chi v5 | `adapters/chi` |
| HTTP server | gorilla/mux | `adapters/mux` |
| RPC | gRPC server + client | `adapters/grpc` |
| HTTP client | net/http `RoundTripper` | `httplog` (in core) |
| HTTP client | Resty v3 | `adapters/resty` |
| Database | GORM | `adapters/gorm` |
| Database | pgx v5 | `adapters/pgx` |
| Tracing | OpenTelemetry | `adapters/otel` |

All HTTP server adapters behave like `httplog`: same field names, same
level-by-status rule, same `X-Request-ID` handling, panic recovery, and skip
lists, so dashboards work across frameworks unchanged. Client and database
adapters pick up `request_id` and other context fields automatically when you
pass the request context.

### Fiber v3

```go
import fiberlog "github.com/techpartners-asia/logger-go/adapters/fiber"

app := fiber.New()
app.Use(fiberlog.New(log, fiberlog.Config{SkipPaths: []string{"/healthz"}}))

app.Get("/orders/:id", func(c fiber.Ctx) error {
	l := fiberlog.FromCtx(c)                     // or logger.FromContext(c.Context())
	l.Info("fetching", "id", c.Params("id"))
	return c.JSON(order)
})
```

Status comes from `*fiber.Error` when a handler returns one, so
`return fiber.ErrNotFound` logs as 404 at WARN. Plain errors log as 500 at
ERROR with the `error` field.

### Gin

```go
import ginlog "github.com/techpartners-asia/logger-go/adapters/gin"

r := gin.New()
r.Use(ginlog.New(log), ginlog.Recovery(log))   // Recovery replaces gin.Recovery

r.GET("/items/:id", func(c *gin.Context) {
	ginlog.FromCtx(c).Info("hit")
})
```

Also logs `route` (`/items/:id`) next to the concrete `path`, and `c.Errors`
under `error`.

### Echo v4

```go
import echolog "github.com/techpartners-asia/logger-go/adapters/echo"

e := echo.New()
e.Use(echolog.New(log, echolog.Config{SkipPaths: []string{"/healthz"}}))

e.GET("/items/:id", func(c echo.Context) error {
	echolog.FromCtx(c).Info("hit")
	return c.JSON(200, item)
})
```

Handler errors go through Echo's `HTTPErrorHandler` first, so the logged
`status` is the one the client saw. Panics are recovered and logged, so
`middleware.Recover()` is optional. Logs `route` (`/items/:id`) too.

### chi, gorilla/mux and other net/http routers

Anything built on `http.Handler` uses `httplog.Middleware` directly:

```go
mux := http.NewServeMux()                 // std library
handler := httplog.Middleware(log)(mux)

r := httprouter.New()                     // httprouter, bunrouter, ...
http.ListenAndServe(":8080", httplog.Middleware(log)(r))
```

The chi and mux adapters are thin wrappers that additionally log the matched
route pattern, which the router only knows after dispatch:

```go
import chilog "github.com/techpartners-asia/logger-go/adapters/chi"

r := chi.NewRouter()
r.Use(chilog.Middleware(log))             // adds route="/users/{id}"
r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
	chilog.FromRequest(r).Info("hit")
})
```

```go
import muxlog "github.com/techpartners-asia/logger-go/adapters/mux"

r := mux.NewRouter()
r.Use(muxlog.Middleware(log))             // adds route="/users/{id}", route_name="getUser"
r.HandleFunc("/users/{id}", h).Name("getUser")
```

Both accept `httplog.Options`.

### gRPC

```go
import grpclog "github.com/techpartners-asia/logger-go/adapters/grpc"

srv := grpc.NewServer(
	grpc.ChainUnaryInterceptor(grpclog.UnaryServerInterceptor(log, grpclog.Config{
		SkipMethods: []string{"/grpc.health.*"},
	})),
	grpc.ChainStreamInterceptor(grpclog.StreamServerInterceptor(log)),
)

conn, _ := grpc.NewClient(addr,
	grpc.WithUnaryInterceptor(grpclog.UnaryClientInterceptor(log)),
	grpc.WithStreamInterceptor(grpclog.StreamClientInterceptor()),
)
```

Fields: `grpc_service`, `grpc_method`, `grpc_type`, `grpc_code`,
`duration_ms`, `remote_addr`, `error`. Request IDs travel in `x-request-id`
metadata both ways. Level is derived from the status code
(`grpclog.DefaultLevelFor`, overridable): client-caused codes INFO,
retryable WARN, server faults ERROR. Panics become `codes.Internal`.

### Resty v3

Resty is the HTTP client used by the other techpartners SDKs.

```go
import restylog "github.com/techpartners-asia/logger-go/adapters/resty"

client := restylog.Install(resty.New(), log, restylog.Config{
	Level:           logger.LevelInfo,         // default DEBUG
	SlowThreshold:   2 * time.Second,
	LogResponseBody: false,                    // bodies off by default
})

resp, err := client.R().SetContext(ctx).Get(url)   // ctx carries request_id
```

One line per call with `method`, `url`, `status`, `duration_ms`, `bytes`,
`attempt` on retries, `error` on transport failure. `X-Request-ID` is
forwarded from the context. Resty's own diagnostics (retries, redirects) go
through the same logger via `SetLogger`.

### GORM

```go
import gormlog "github.com/techpartners-asia/logger-go/adapters/gorm"

db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
	Logger: gormlog.New(log, gormlog.Config{SlowThreshold: 200 * time.Millisecond}),
})
```

Queries log at DEBUG (`sql`, `rows`, `duration_ms`), slow ones at WARN with
`slow=true`, failures at ERROR. `ErrRecordNotFound` is not treated as an
error. Use `db.WithContext(ctx)` so request fields appear. Set
`ParameterizedQueries: true` to log `?` instead of bound values.

### pgx

```go
import pgxlog "github.com/techpartners-asia/logger-go/adapters/pgx"

cfg, _ := pgxpool.ParseConfig(dsn)
cfg.ConnConfig.Tracer = pgxlog.NewTracer(log, pgxlog.Config{SlowThreshold: 200 * time.Millisecond})
pool, _ := pgxpool.NewWithConfig(ctx, cfg)
```

Same levels and fields as the GORM adapter. Arguments are not logged unless
`LogArgs: true`. `pgxlog.NewLogger(log)` implements `tracelog.Logger` if you
already use `tracelog.TraceLog`.

### OpenTelemetry

```go
import otellog "github.com/techpartners-asia/logger-go/adapters/otel"

log := logger.MustNew(logger.WithService("api"), otellog.WithTrace())

log.InfoContext(ctx, "charged")
// {"msg":"charged","trace_id":"4bf92f35...","span_id":"00f067aa...","trace_flags":"01"}
```

The adapter depends only on `go.opentelemetry.io/otel/trace`.
