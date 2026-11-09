# Production

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

On servers, the directory must be `/var/log/apps/<project>` for logs to reach
Loki. See [Production: ship logs to Loki](#production-ship-logs-to-loki).

`Tee` is the usual choice in containers: stdout/stderr is shipped by the
platform while the file is there for `tail -f` on the box. Without `Tee` the
file is the only output.

The writer is exported as `logger.RotatingWriter` for use with any logger,
and `WithOutputCloser` still accepts third-party writers such as lumberjack
if you prefer them.

## Production: ship logs to Loki

> **Required for every service on a server.** Logs only reach Loki and
> log-analytic if they are written as JSON to
> `/var/log/apps/<project>/<service>.log`. Anywhere else, they are not collected.

Each server runs Alloy and Loki (`deploy/test-server/docker-compose.yml` in
log-analytic). Alloy reads every `/var/log/apps/*/*.log` and sends it to Loki
with three labels:

| Label | Comes from |
|---|---|
| `project` | The folder name: `/var/log/apps/shop/...` gives `shop` |
| `service` | The `service` field on each line (`LOG_SERVICE`) |
| `env` | The `env` field on each line (`LOG_ENV`) |

Set these four variables:

```bash
LOG_FORMAT=json                 # Alloy only parses JSON
LOG_SERVICE=orders-api          # file name and `service` label
LOG_ENV=prod                    # `env` label
LOG_DIR=/var/log/apps/shop      # /var/log/apps/<project>
```

That writes `/var/log/apps/shop/orders-api.log`. In code, use
`logger.WithConfig(logger.ConfigFromEnv())` as in [Quick start](../README.md#quick-start),
or pass the same values with `WithService`, `WithEnv` and `WithDir`.

Create the project folder on the server once:

```bash
sudo mkdir -p /var/log/apps/shop
```

If the service runs in Docker, mount the folder to the same path, or the file
stays inside the container where Alloy cannot see it:

```yaml
services:
  orders-api:
    environment:
      LOG_FORMAT: json
      LOG_SERVICE: orders-api
      LOG_ENV: prod
      LOG_DIR: /var/log/apps/shop
    volumes:
      - /var/log/apps/shop:/var/log/apps/shop
```

Rules:

- **One project per folder.** The project comes from the folder name, not from
  the log line.
- **Keep the default rotation names.** Alloy skips rotated backups
  (`orders-api-20261016T122446.489.log`) because it has already read their
  lines from the live file. `Filename` may change, but it must end in `.log`.
  `Compress` is fine.
- **Do not combine the file with a `log.project` container label.** That label
  makes Alloy also collect the container's stdout, so with `LOG_FILE_TEE=true`
  every line reaches Loki twice. Use the file or the label, not both.
