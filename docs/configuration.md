# Configuration

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
