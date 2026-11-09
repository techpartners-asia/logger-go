# Logging

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
values. An `error` passed on its own is logged under `"error"`:

```go
log.Error("charge failed", err, "order_id", 991)
// {"level":"ERROR","msg":"charge failed","error":"card declined","order_id":991}
```

Attribute constructors are re-exported so you rarely import slog:

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
