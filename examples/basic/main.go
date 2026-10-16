// Command basic shows the core logger features. Run it twice:
//
//	go run ./examples/basic
//	LOG_FORMAT=console LOG_LEVEL=trace go run ./examples/basic
package main

import (
	"context"
	"errors"
	"time"

	"github.com/techpartners-asia/logger-go"
)

func main() {
	log := logger.MustNew(
		logger.WithConfig(logger.ConfigFromEnv()), // LOG_LEVEL, LOG_FORMAT, ...
		logger.WithService("example"),
		logger.WithVersion("1.0.0"),
		logger.WithRedaction(),
		logger.WithStackTrace(logger.LevelError),
	)
	defer log.Close()
	logger.SetDefault(log)

	log.Trace("very verbose")
	log.Debug("connecting", "dsn", "postgres://db:5432/app")
	log.Info("server started", "port", 8080, "workers", 4)

	// Sensitive keys are masked automatically.
	log.Info("user login", "user", "bob", "password", "hunter2", "Authorization", "Bearer xyz")

	// Child loggers and groups.
	orders := log.Named("orders").With("tenant", "acme")
	orders.Warn("inventory low", "sku", "A-1", "left", 3)
	orders.WithGroup("payment").Info("charged", "amount", 19.99, "currency", "MNT")

	// Context fields follow the request through every layer.
	ctx := logger.ContextWithFields(context.Background(),
		logger.String(logger.KeyRequestID, "req-123"),
		logger.String(logger.KeyUserID, "u-42"),
	)
	process(ctx)

	// Errors get a stack when WithStackTrace is enabled.
	log.Error("charge failed", logger.Err(errors.New("card declined")), "order_id", 991)

	// Timing helper.
	start := time.Now()
	time.Sleep(12 * time.Millisecond)
	log.Info("job done", logger.Duration(time.Since(start)))
}

func process(ctx context.Context) {
	// No logger passed around: pick it from the context.
	logger.FromContext(ctx).InfoContext(ctx, "processing", "step", 1)
	logger.InfoContext(ctx, "processing", "step", 2) // package-level works too
}
