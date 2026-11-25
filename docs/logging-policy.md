# Logging policy

How to meet the company logging procedure (Лог бүртгэлийн үйл ажиллагааны
журам, 05/С-13/01-26/ADD) with this SDK. The procedure is the source of truth.
This page maps each rule to the option, key or habit that satisfies it.

The procedure and its ADD-01 form are in ERP under Дотоод хэм хэмжээ →
Дүрэм, журам → Журам → C ангилал → Лог бүртгэлийн үйл ажиллагааны журам →
Маягт. The information security (МАБ) engineer oversees every step.

## Never log in plain text

Passwords, OTPs, access tokens, API keys, private keys, session cookies, card
data, personal data and confidential data must not appear in a log. If a value
must appear, mask it (`****`) or hash it.

Turn on redaction in every service:

```go
logger.WithRedaction("phone", "email", "register", "national_id", "address")
```

or `LOG_REDACT_KEYS=phone,email,register,national_id,address`.

The default key set already covers passwords, OTPs, tokens, API keys, private
keys, cookies, sessions and card numbers (full list in
[Handlers](handlers.md#redaction)). It does not cover personal data, because
field names differ between services. Add your own PII keys as above.

What redaction does not do, and you must:

- **Keep secrets out of the message.** Redaction is key-based. It never sees
  `log.Info("login user=bat password=" + pw)`. Put values in attributes.
- **Hash when you need to correlate.** The SDK masks, it does not hash. To
  follow one customer across lines without logging who they are, hash before
  logging: `"email_hash", sha256hex(email)`.
- **Show only a suffix when support needs it.** `KeepLast: 4` turns a card
  number into `****1111`.
- **Do not log whole request or response bodies.** Nested fields are redacted
  only when they arrive as slog groups. A JSON string or a struct logged with
  `logger.Any` is written as is.

## Always log in production

| Event | Example `action` |
|---|---|
| Login success and failure, MFA | `auth.login`, `auth.mfa` |
| Granting, changing or revoking access | `access.grant`, `access.revoke` |
| Privileged actions and admin settings | `admin.config.update` |
| Changing, deleting or exporting data | `data.delete`, `data.export` |
| Errors, exceptions, API auth failures | `api.auth` |
| Deployment, rollback, hotfix | `deploy.release`, `deploy.rollback` |
| Firewall, IAM and schema changes | `db.migrate`, `iam.update` |
| Backup, restore, snapshot | `backup.run`, `backup.restore` |

Log these at `Info` or above so production log levels keep them. Failures
are `Warn`, or `Error` when a person should look.

Sampling drops repeated `Info` and `Warn` lines by default. A logger that
writes audit events either runs without sampling or exempts `Info`:

```go
info := logger.LevelInfo
logger.WithSampling(logger.Sampling{Initial: 100, Thereafter: 100, Tick: time.Second, ExemptLevel: &info})
```

## Fields on every audit line

The procedure asks each line to say when, in which environment, who, what,
the result, the IP and the error code.

| Required | Key | Set by |
|---|---|---|
| When | `time` | Every record. Use `WithUTC()` or keep server clocks on NTP. |
| Environment | `env` | `LOG_ENV` or `WithEnv`, once at startup |
| Who | `user_id` | `logger.KeyUserID`. Put it in the context in auth middleware. |
| What | `action` | `logger.KeyAction` |
| Result | `outcome` | `logger.KeyOutcome`: `logger.OutcomeSuccess` or `logger.OutcomeFailure` |
| IP | `remote_addr` | `logger.KeyRemoteAddr`. Middleware writes it on access lines only. |
| Error code | `error_code` | `logger.KeyErrorCode`, beside `error` |

The HTTP middleware puts `remote_addr` on its access line, not on lines your
handlers log. Add it to the context in middleware so audit lines carry it:

```go
ctx = logger.ContextWithFields(ctx,
	logger.String(logger.KeyUserID, user.ID),
	logger.String(logger.KeyRemoteAddr, clientIP),
)
```

Then an audit line is:

```go
log.WarnContext(ctx, "login failed",
	logger.KeyAction, "auth.login",
	logger.KeyOutcome, logger.OutcomeFailure,
	logger.KeyErrorCode, "INVALID_OTP",
)
```

```json
{"time":"2026-11-25T03:12:09.114Z","level":"WARN","msg":"login failed","service":"auth-api","env":"prod","action":"auth.login","outcome":"failure","error_code":"INVALID_OTP","user_id":"u-42","remote_addr":"10.2.0.17"}
```

## Retention

| Log | Primary | Archive |
|---|---|---|
| Logins and admin actions | 1 year | 1 year |
| Deployments, releases, rollbacks | 1 year | 1 year |
| Database change logs | 3 months | 1 year |
| System, API and server errors | 3 months | 6 months |
| Test environments | 1 month | 3 months |
| Anything tied to an incident | 3 years | 1 year after the incident closes |

Retention is enforced in Loki and the archive, not by the SDK. The rotating
file is a buffer for Alloy to read, and its default `MaxAgeDays: 30` does not
meet any row above. Never treat the local file as the retained copy.

## Duties of the system owner

- **No debug in production.** Run prod at `info`. To debug a live issue, raise
  the level with `log.SetLevel(logger.LevelDebug)` and set it back afterwards.
  Never ship `LOG_LEVEL=debug` or `trace` to prod.
- **Fix gaps.** If logs stop arriving in Loki or the disk fills, fix it. A
  gap in the logs is a gap in the evidence.
- **Register new systems.** A new system, API or database goes on the ADD-01
  list. The system owner fills it in, the МАБ engineer reviews it, and the
  CTO approves it.
- **Sync clocks.** Server time must come from NTP so lines from different
  services line up.

## Logs are evidence

Do not delete, edit or overwrite logs. The SDK opens files in append mode and
rotation renames the file without rewriting lines. `MaxBackups` and
`MaxAgeDays` do delete old local backups, which is safe only because Alloy has
already shipped their lines to Loki.

Sources: procedure 05/С-13/01-26/ADD, clauses 1.3.3, 6.1.4, 6.2.1, 7.1.4 to
7.1.7, 7.2.4 and 8.1.2.
