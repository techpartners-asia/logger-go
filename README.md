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

## Contents

- [Quick start](#quick-start)
- [Output formats](#output-formats)
- [Configuration](#configuration)
  - [Options](#options)
  - [Environment variables](#environment-variables)
  - [Config struct](#config-struct)
- [Logging API](#logging-api)
- [Context propagation](#context-propagation)
- [Redaction](#redaction)
- [Sampling](#sampling)
- [Stack traces](#stack-traces)
- [Writing to files](#writing-to-files)
- [Multiple outputs](#multiple-outputs)
- [Interop: slog, std log, io.Writer](#interop-slog-std-log-iowriter)
- [HTTP middleware (net/http)](#http-middleware-nethttp)
- [Adapters](#adapters)
  - [Fiber v3](#fiber-v3)
  - [Gin](#gin)
  - [Echo v4](#echo-v4)
  - [chi, gorilla/mux and other net/http routers](#chi-gorillamux-and-other-nethttp-routers)
  - [gRPC](#grpc)
  - [Resty v3](#resty-v3)
  - [GORM](#gorm)
  - [pgx](#pgx)
  - [OpenTelemetry](#opentelemetry)
- [Testing with logtest](#testing-with-logtest)
- [Conventions for services](#conventions-for-services)
- [Migrating from zap / zerolog / logrus](#migrating-from-zap--zerolog--logrus)
- [Architecture](#architecture)
- [Performance](#performance)
- [Development](#development)
- [Releasing](#releasing)

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
	log.Error("charge failed", logger.Err(errors.New("card declined")), "order_id", 991)
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

## Output formats

| Format | Option | When |
|---|---|---|
| `json` | `WithFormat(logger.FormatJSON)` | Production. One object per line, parsed by Loki, Elastic, Datadog, CloudWatch. |
| `console` | `WithFormat(logger.FormatConsole)` | Local development. Colors, 3-letter levels, `group.key=value` flattening, stacks on indented lines. |
| `text` | `WithFormat(logger.FormatText)` | slog's logfmt-style `key=value`. |

Built-in keys are `time`, `level`, `msg`, `source`. Rename them for a backend
schema with `WithKeyNames(logger.ElasticKeyNames)` (`@timestamp`, `log.level`,
`message`) or `logger.DatadogKeyNames` (`timestamp`, `status`, `message`), or
supply your own `KeyNames`.

Levels, lowest to highest: `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`,
`PANIC`. They are plain `slog.Level` values, so `slog`-aware tooling understands
them, and the extended ones print by name rather than as `DEBUG-4`.

---

## Configuration

Three equivalent ways, which you can mix. Later options win.

### Options

```go
log, err := logger.New(
	logger.WithLevel(logger.LevelDebug),        // or WithLevelString("debug")
	logger.WithFormat(logger.FormatJSON),
	logger.WithOutput(os.Stdout),               // default os.Stderr
	logger.WithDir("/var/log/orders"),          // rotating file <dir>/<service>.log
	logger.WithFile(logger.FileConfig{Dir: "/var/log/orders", Compress: true, Tee: true}),
	logger.WithService("orders"),               // "service" field
	logger.WithEnv("staging"),                  // "env" field
	logger.WithVersion("1.4.0"),                // "version" field
	logger.WithProcessInfo(),                   // "host" and "pid" fields
	logger.WithFields(logger.String("region", "ap-east-1")),
	logger.WithSource(true),                    // "source":"pkg/file.go:42"
	logger.WithUTC(),
	logger.WithTimeFormat(time.RFC3339Nano),
	logger.WithKeyNames(logger.ElasticKeyNames),
	logger.WithColor(false),                    // console only; default auto
	logger.WithRedaction("iban", "phone"),      // defaults + extra keys
	logger.WithSampling(logger.Sampling{Initial: 100, Thereafter: 100}),
	logger.WithStackTrace(logger.LevelError),
	logger.WithContextExtractor(myTraceExtractor),
	logger.WithContextValue("tenant_id", tenantKey{}),
	logger.WithHandler(customSlogHandler),      // bring your own base handler
	logger.WithMiddleware(myHandlerMiddleware),
	logger.WithReplaceAttr(myReplaceAttr),
	logger.WithExitFunc(func(int) {}),          // for tests of Fatal
)
```

`MustNew` panics instead of returning an error. The only error source is an
output file that cannot be opened.

### Environment variables

`logger.ConfigFromEnv()` reads these. Pass the result to `WithConfig`.

| Variable | Values | Default |
|---|---|---|
| `LOG_LEVEL` | `trace` `debug` `info` `warn` `error` `fatal` | `info` |
| `LOG_FORMAT` | `json` `console` `text` | `json` |
| `LOG_OUTPUT` | `stderr` `stdout` or a file path (opened append, closed by `Close`) | `stderr` |
| `LOG_SERVICE` (or `SERVICE_NAME`, `APP_NAME`) | string | |
| `LOG_ENV` (or `APP_ENV`, `ENVIRONMENT`, `ENV`) | string | |
| `LOG_VERSION` (or `APP_VERSION`, `SERVICE_VERSION`) | string | |
| `LOG_SOURCE` | bool | `false` |
| `LOG_COLOR` | bool; `NO_COLOR` also respected | auto |
| `LOG_UTC` | bool | `false` |
| `LOG_TIME_FORMAT` | Go layout | RFC3339 ms |
| `LOG_PROCESS_INFO` | bool | `false` |
| `LOG_REDACT` | bool | `false` |
| `LOG_REDACT_KEYS` | comma list; implies `LOG_REDACT=true` | |
| `LOG_SAMPLING_INITIAL` / `LOG_SAMPLING_THEREAFTER` | int | off |
| `LOG_STACK_TRACE_LEVEL` | level name | off |
| `LOG_KEYS` | `default` `elastic` `datadog` | `default` |
| `LOG_DIR` | directory; enables a rotating log file | off |
| `LOG_FILE` | file name inside `LOG_DIR` | `<service>.log` |
| `LOG_FILE_MAX_SIZE_MB` | rotate above this size | `100` |
| `LOG_FILE_MAX_BACKUPS` | rotated files to keep | `10` |
| `LOG_FILE_MAX_AGE_DAYS` | delete rotated files older than this | `30` |
| `LOG_FILE_DAILY` | bool; also rotate at midnight | `false` |
| `LOG_FILE_COMPRESS` | bool; gzip rotated files | `false` |
| `LOG_FILE_TEE` | bool; also write to stderr | `false` |

Recommended per environment:

```
# production
LOG_LEVEL=info LOG_FORMAT=json LOG_REDACT=true LOG_SAMPLING_INITIAL=100 LOG_SAMPLING_THEREAFTER=100 LOG_STACK_TRACE_LEVEL=error
# local
LOG_LEVEL=debug LOG_FORMAT=console LOG_SOURCE=true
```

### Config struct

`logger.Config` has JSON and YAML tags, so it can sit inside your service's
config file. `ConfigFromLookup(fn)` builds it from any key/value source.

```go
var cfg struct {
	Log logger.Config `yaml:"log"`
}
log := logger.MustNew(logger.WithConfig(cfg.Log))
```

---

## Logging API

```go
log.Trace / Debug / Info / Warn / Error(msg string, args ...any)
log.Fatal(msg, args...)   // logs at FATAL, flushes, os.Exit(1)
log.Panic(msg, args...)   // logs at PANIC, flushes, panic(msg)
log.Log(ctx, level, msg, args...)
log.LogAttrs(ctx, level, msg, attrs...)   // allocation-free

log.TraceContext / DebugContext / InfoContext / WarnContext / ErrorContext(ctx, msg, args...)
log.Infof / Debugf / ...(format, a...)    // for migrating fmt-style code
```

`args` follow slog: alternating `"key", value` pairs and/or `logger.Attr`
values. Attribute constructors are re-exported so you rarely import slog:

```go
logger.String, Int, Int64, Uint64, Float64, Bool, Time, Any, Group
logger.Err(err)                 // "error": err.Error(); nil is omitted
logger.NamedErr("cause", err)
logger.Duration(d)              // "duration_ms": 12.345 (numbers aggregate; "12ms" strings do not)
```

Derived loggers share the handler and level and are cheap to create:

```go
log.With("tenant", id)                 // attrs on every record
log.WithAttrs(logger.String("k", "v"))
log.WithGroup("http")                  // nests following attrs: {"http":{"status":200}}
log.WithError(err)
log.Named("orders")                    // "logger":"orders"; nested: "orders.repo"
```

Level control at runtime, for example from an admin endpoint or SIGUSR1:

```go
log.SetLevel(logger.LevelDebug)
log.SetLevelString("warn")
log.Level()
log.Enabled(ctx, logger.LevelDebug)   // guard expensive argument building
```

`SetLevel` affects every logger derived from the same `New` call.

Global convenience, after `logger.SetDefault(log)`:

```go
logger.Info("x")               logger.With("k", "v")
logger.InfoContext(ctx, "x")   logger.Named("pkg")
logger.Default()               logger.SetLevel(logger.LevelDebug)
```

---

## Context propagation

Request IDs, user IDs and trace IDs should appear on every line without being
threaded through every function signature. Two mechanisms, both keyed on the
`context.Context` you already pass around:

**1. A logger in the context.** Middleware stores a request-scoped logger;
code deeper in the stack picks it up.

```go
ctx = logger.NewContext(ctx, log.With("request_id", id))
...
logger.FromContext(ctx).Info("saving")   // never nil; falls back to Default()
```

**2. Fields in the context.** Attributes stored this way are added to every
record logged with that context through any `*Context` method, regardless of
which logger instance is used, including third-party slog users.

```go
ctx = logger.ContextWithFields(ctx, logger.String("request_id", id), logger.String("user_id", uid))
ctx = logger.ContextWith(ctx, "step", 2)   // variadic form

log.InfoContext(ctx, "processing")         // includes request_id, user_id, step
slog.InfoContext(ctx, "from a library")    // also includes them (after SetDefault)
```

**Extractors** derive attributes from a context on every record. Use them to
pull trace IDs from your tracing library (see the OpenTelemetry adapter) or
to map an existing context key to a field:

```go
logger.WithContextValue("tenant_id", tenantCtxKey{})
logger.WithContextExtractor(func(ctx context.Context) []logger.Attr {
	if u, ok := auth.UserFromContext(ctx); ok {
		return []logger.Attr{logger.String("user_id", u.ID)}
	}
	return nil
})
```

Only the `*Context` methods see context data. `log.Info(...)` without a
context never will, by design.

---

## Redaction

`WithRedaction()` masks values whose key matches a sensitive set, wherever
they appear: record attributes, `With(...)` attributes, nested groups. Matching
is case-insensitive and ignores `-`, `_`, `.` and spaces, so `Authorization`,
`x-api-key`, `apiKey` and `card_number` are all caught.

Default keys: password, passwd, pwd, secret, token, access_token,
refresh_token, id_token, authorization, auth, api_key, apikey, x_api_key,
client_secret, private_key, credit_card, card_number, card, pan, cvv, cvc, pin,
ssn, cookie, set_cookie, session, session_id, otp, bearer, credentials,
signature.

```go
logger.WithRedaction("iban", "phone")                     // defaults + extras
logger.WithRedactionConfig(logger.Redaction{
	Keys:            []string{"iban"},
	DisableDefaults: true,
	Replacement:     "***",
	KeepLast:        4,          // "4111111111111111" -> "****1111"
	Match:           func(normalizedKey string) bool { ... },  // full control
})
```

Redaction is key-based. It does not scan free-text messages, so keep secrets
out of `msg` and put them in attributes where the handler can see them.

---

## Sampling

A hot loop logging the same message a million times a minute will take down a
log pipeline before it takes down the service. Sampling caps repeats per
`(level, message)` per time window:

```go
logger.WithSampling(logger.Sampling{
	Initial:    100,          // always log the first 100 per window
	Thereafter: 100,          // then 1 in 100
	Tick:       time.Second,  // window
	// ExemptLevel: default ERROR; records at or above are never dropped
	// OnDropped: func(level, msg string, n uint64) { metrics.Add(n) }
})
```

Distinct messages do not affect each other, and counters are shared across
`With` children so sampling is per message, not per logger instance.

---

## Stack traces

```go
logger.WithStackTrace(logger.LevelError)
```

Adds a `stack` attribute to records at or above the level. Frames inside this
package and `log/slog` are trimmed so the trace starts at your call site. The
console format prints it on indented lines under the record.

---

## Writing to files

Give the SDK a directory at init and it manages the file for you:

```go
log := logger.MustNew(
	logger.WithService("orders-api"),
	logger.WithDir("/var/log/techpartners"),   // -> /var/log/techpartners/orders-api.log
)
defer log.Close() // flushes and closes the file
```

The directory is created if missing. The file rotates at 100 MB, 10 backups
are kept for 30 days, and backups are named `orders-api-20261016T122446.489.log`.
Tune it with `WithFile`:

```go
logger.WithFile(logger.FileConfig{
	Dir:        "/var/log/techpartners",
	Filename:   "orders.log",   // default <service>.log
	MaxSizeMB:  50,             // rotate above this size; -1 disables
	MaxBackups: 7,              // -1 keeps all
	MaxAgeDays: 14,             // -1 keeps all
	Daily:      true,           // also rotate at local midnight
	Compress:   true,           // gzip backups in the background
	Tee:        true,           // keep writing to stderr as well
})
```

Or from the environment: `LOG_DIR=/var/log/techpartners LOG_FILE_COMPRESS=true`.
In a config file the same fields sit under `log.dir` / `log.file`.

`Tee` is the usual choice in containers: stdout/stderr is shipped by the
platform while the file is there for `tail -f` on the box. Without `Tee` the
file is the only output.

The writer is exported as `logger.RotatingWriter` for use with any logger,
and `WithOutputCloser` still accepts third-party writers such as lumberjack
if you prefer them.

---

## Multiple outputs

Fan out to several destinations with different levels using the exported
handlers and `WithHandler`:

```go
jsonFile := slog.NewJSONHandler(file, &slog.HandlerOptions{Level: logger.LevelInfo})
console  := logger.NewConsoleHandler(os.Stderr, &logger.ConsoleOptions{Level: logger.LevelDebug})
alerts   := logger.NewLevelHandler(logger.LevelError, slackHandler)

log := logger.MustNew(
	logger.WithHandler(logger.NewMultiHandler(jsonFile, console, alerts)),
	logger.WithLevel(logger.LevelDebug),
	logger.WithService("api"),   // service/env fields and redaction still apply
)
```

For file rotation see [Writing to files](#writing-to-files).

Exported handlers, all usable standalone with plain slog too:
`NewConsoleHandler`, `NewRedactHandler`, `NewSamplingHandler`,
`NewStackHandler`, `NewMultiHandler`, `NewLevelHandler`.

---

## Interop: slog, std log, io.Writer

```go
sl := log.Slog()                       // *slog.Logger over the same pipeline
h  := log.Handler()                    // slog.Handler
l2 := logger.FromSlog(existingSlog)    // wrap the other way

std := log.Std(logger.LevelInfo)       // *log.Logger (http.Server.ErrorLog, etc.)
w   := log.Writer(logger.LevelDebug)   // io.Writer; each line becomes a record

restore := logger.RedirectStdLog(log, logger.LevelInfo)  // capture the global "log" package
defer restore()
```

`logger.SetDefault(log)` also calls `slog.SetDefault`, so third-party code
using `slog.Info` shares the output, fields and redaction.

---

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

---

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

---

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

---

## Conventions for services

These keep logs uniform across techpartners backends. The constants live in
the package as `logger.Key*`.

| Key | Meaning |
|---|---|
| `service`, `env`, `version` | Set once in `New`. Every service, always. |
| `request_id`, `trace_id`, `span_id` | Correlation. Set by middleware/adapters. |
| `user_id` | Authenticated principal. |
| `error` | `err.Error()` via `logger.Err`. Never log `err` with `%v` in the message. |
| `duration_ms` | Numeric milliseconds via `logger.Duration`. |
| `method`, `path`, `status`, `bytes`, `remote_addr`, `user_agent` | HTTP. |
| `logger` | Component name from `Named`. |

Rules of thumb:

- Message is a constant, lower-case phrase. Variables go in attributes:
  `log.Info("order created", "order_id", id)`, not `log.Infof("order %d created", id)`.
- One line per request from the middleware. Handlers add context, not
  duplicate access logs.
- `Error` is for things a human should look at. Expected failures (validation,
  404s) are `Warn` or `Info`.
- `Fatal` only in `main` during startup. Elsewhere return errors.
- Always enable `WithRedaction()` in services that touch customer data.

---

## Migrating from zap / zerolog / logrus

| You wrote | Write now |
|---|---|
| `zap.L().Info("x", zap.String("k", v), zap.Error(err))` | `log.Info("x", "k", v, logger.Err(err))` |
| `logger.With(zap.String("k", v))` | `log.With("k", v)` |
| `zerolog.Ctx(ctx).Info().Str("k", v).Msg("x")` | `logger.FromContext(ctx).Info("x", "k", v)` |
| `log.Info().Err(err).Msg("x")` | `log.Error("x", logger.Err(err))` |
| `logrus.WithFields(logrus.Fields{"k": v}).Info("x")` | `log.Info("x", "k", v)` |
| `logrus.WithError(err).Error("x")` | `log.WithError(err).Error("x")` |
| `zap.Sugar().Infof("n=%d", n)` | `log.Infof("n=%d", n)` (then move to attributes) |
| `logger.Sync()` | `log.Close()` |
| `zap.AddCallerSkip` | not needed; source is computed per call |

Libraries that accept `*zap.Logger` only: keep zap for them, or use their
slog option if one exists (`log.Slog()`). Libraries that accept an
`io.Writer` or `*log.Logger`: `log.Writer(level)` / `log.Std(level)`.

---

## Architecture

```
Logger (level var, name, shared lifecycle)
  └─ levelVarHandler        cheap Enabled() gate; makes SetLevel work for any handler
      └─ user middlewares   WithMiddleware, outermost first
          └─ contextHandler  adds ContextWithFields attrs, WithContextValue, extractors
              └─ SamplingHandler   drops repeats (ERROR+ exempt)
                  └─ StackHandler      adds "stack" at >= level
                      └─ RedactHandler     masks sensitive keys, incl. WithAttrs and groups
                          └─ base handler (+ static fields: service, env, version, host, pid)
                                JSON | Console | Text | WithHandler(...)
```

Design choices:

- **slog underneath, not alongside.** Everything is a `slog.Handler`. There is
  no second attribute type, no adapter layer, and any slog handler from the
  ecosystem can be the base or a middleware.
- **Source locations are computed by this package**, not by slog, so wrappers
  (`Info`, package-level `logger.Info`, `Writer`) always report the real
  caller.
- **Context data only flows through `*Context` methods.** Implicit goroutine
  state would be surprising and slow.
- **Redaction runs after context injection and sampling**, so secrets coming
  from context fields are masked too, and sampling decisions do not depend on
  values.
- **Static fields are attached to the base handler**, so they are serialized
  once per `New` rather than per record.
- **Adapters are separate modules.** Core stays dependency-free and semver
  stable while framework versions move.

Layout:

```
logger.go            Logger, New, options wiring, lifecycle
options.go           Option funcs, KeyNames presets
config.go            Config struct, env loading
file.go              RotatingWriter, FileConfig, WithDir/WithFile
level.go             levels, parsing, names
attrs.go             Attr helpers, well-known keys
context.go           NewContext/FromContext, ContextWithFields, contextHandler
global.go            Default(), SetDefault, package-level funcs
handler_console.go   ConsoleHandler
handler_redact.go    RedactHandler, Redaction
handler_sample.go    SamplingHandler, Sampling
handler_stack.go     StackHandler
handler_multi.go     MultiHandler, LevelHandler
stdlog.go            Writer, Std, RedirectStdLog
httplog/             net/http middleware and RoundTripper
logtest/             Recorder for tests
adapters/*           separate modules: fiber gin echo chi mux grpc resty gorm pgx otel
examples/basic, examples/http
```

---

## Performance

Apple M-series, Go 1.27, JSON to a discard writer:

| Scenario | ns/op | allocs/op |
|---|---|---|
| Disabled level (`Info` below threshold) | 9 | 0 |
| JSON, 3 attrs, service field | ~800 | 4 |
| JSON + redaction + sampling | ~810 | 7 |

`log.LogAttrs` with pre-built attrs is the allocation-free path. Guard
expensive argument construction with `log.Enabled(ctx, level)`.

Run `make bench`.

---

## Development

```
make test        # all modules, race detector
make lint        # go vet + gofmt (+ golangci-lint if installed)
make bench
make examples
```

The repo is a Go workspace (`go.work`) so adapters build against the local
core. Adapter `go.mod` files carry a `replace ../..` directive for the same
reason; consumers ignore it and resolve the tagged core version.

Requirements: Go 1.24+ for the core module; Go 1.25+ for the Fiber adapter
(Fiber v3's own requirement).

---

## Releasing

Tag the core and each adapter you changed. Adapters pin the core version in
their `go.mod`, so bump that first when the core changes.

```
git tag v0.1.0
git tag adapters/fiber/v0.1.0
git tag adapters/gin/v0.1.0
git tag adapters/grpc/v0.1.0
git tag adapters/otel/v0.1.0
# ... one tag per adapter you changed (echo, chi, mux, resty, gorm, pgx)
git push --tags
```

See [CHANGELOG.md](CHANGELOG.md).

## License

MIT. See [LICENSE](LICENSE).
