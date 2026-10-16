# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Adapters: Echo v4, chi v5, gorilla/mux (route patterns on top of httplog),
  Resty v3 client, GORM logger, pgx v5 tracer and tracelog logger.
- `WithDir` / `WithFile` and `FileConfig`: built-in rotating log file (size
  and daily rotation, max backups, max age, gzip, tee to stderr). Exported
  `RotatingWriter`. `LOG_DIR` and `LOG_FILE_*` environment variables;
  `Config.Dir` / `Config.File`.

## [0.1.0] - 2026-10-16

### Added
- Core `logger` package on `log/slog`: JSON, console and text formats, TRACE
  through PANIC levels, runtime level switching, `Named`/`With`/`WithGroup`,
  `Fatal`/`Panic`, printf helpers.
- Options API plus `Config` struct and `ConfigFromEnv` (`LOG_*` variables).
- Context propagation: `NewContext`/`FromContext`, `ContextWithFields`,
  `WithContextValue`, `WithContextExtractor`.
- Handlers: `RedactHandler`, `SamplingHandler`, `StackHandler`,
  `MultiHandler`, `LevelHandler`, `ConsoleHandler`.
- Key name presets for Elastic and Datadog.
- Interop: `Slog()`, `FromSlog`, `Writer`, `Std`, `RedirectStdLog`.
- `httplog`: net/http middleware with request IDs, panic recovery, slow
  request detection, and a logging `RoundTripper` that propagates request IDs.
- `logtest`: in-memory recorder for assertions.
- Adapters as separate modules: Fiber v3, Gin, gRPC (server and client
  interceptors), OpenTelemetry trace correlation.
- Examples: `examples/basic`, `examples/http`.
