# Architecture

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
