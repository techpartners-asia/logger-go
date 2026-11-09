# Handlers

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

## Stack traces

```go
logger.WithStackTrace(logger.LevelError)
```

Adds a `stack` attribute to records at or above the level. Frames inside this
package and `log/slog` are trimmed so the trace starts at your call site. The
console format prints it on indented lines under the record.

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

For file rotation see [Writing to files](production.md#writing-to-files).

Exported handlers, all usable standalone with plain slog too:
`NewConsoleHandler`, `NewRedactHandler`, `NewSamplingHandler`,
`NewStackHandler`, `NewMultiHandler`, `NewLevelHandler`.
