# Conventions

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
| `action`, `outcome`, `error_code` | Audit events. See [Logging policy](logging-policy.md). |

Rules of thumb:

- Message is a constant, lower-case phrase. Variables go in attributes:
  `log.Info("order created", "order_id", id)`, not `log.Infof("order %d created", id)`.
- One line per request from the middleware. Handlers add context, not
  duplicate access logs.
- `Error` is for things a human should look at. Expected failures (validation,
  404s) are `Warn` or `Info`.
- `Fatal` only in `main` during startup. Elsewhere return errors.
- Always enable `WithRedaction()` in services that touch customer data, and
  add your PII keys. See [Logging policy](logging-policy.md).

## Migrating from zap / zerolog / logrus

| You wrote | Write now |
|---|---|
| `zap.L().Info("x", zap.String("k", v), zap.Error(err))` | `log.Info("x", "k", v, err)` |
| `logger.With(zap.String("k", v))` | `log.With("k", v)` |
| `zerolog.Ctx(ctx).Info().Str("k", v).Msg("x")` | `logger.FromContext(ctx).Info("x", "k", v)` |
| `log.Info().Err(err).Msg("x")` | `log.Error("x", err)` |
| `logrus.WithFields(logrus.Fields{"k": v}).Info("x")` | `log.Info("x", "k", v)` |
| `logrus.WithError(err).Error("x")` | `log.WithError(err).Error("x")` |
| `zap.Sugar().Infof("n=%d", n)` | `log.Infof("n=%d", n)` (then move to attributes) |
| `logger.Sync()` | `log.Close()` |
| `zap.AddCallerSkip` | not needed; source is computed per call |

Libraries that accept `*zap.Logger` only: keep zap for them, or use their
slog option if one exists (`log.Slog()`). Libraries that accept an
`io.Writer` or `*log.Logger`: `log.Writer(level)` / `log.Std(level)`.
