// Package logger is the techpartners.asia structured logging SDK for Go
// backends.
//
// It is built on the standard library's log/slog, so it interoperates with any
// library that accepts a slog.Handler or *slog.Logger, and it adds the pieces
// every production service needs:
//
//   - JSON output for production, a readable colored console for development
//   - service/env/version fields on every record
//   - context propagation: request IDs, trace IDs and per-request fields travel
//     through context.Context and appear automatically on every log line
//   - redaction of sensitive keys (passwords, tokens, card numbers, ...)
//   - sampling to protect log pipelines from hot loops
//   - stack traces for errors, runtime level switching, Fatal/Panic helpers
//   - adapters for net/http (in this module) and Fiber, Gin, gRPC and
//     OpenTelemetry (separate modules under adapters/)
//
// Quick start:
//
//	log := logger.MustNew(
//		logger.WithService("payments-api"),
//		logger.WithEnv("production"),
//	)
//	defer log.Close()
//
//	log.Info("server started", "port", 8080)
//	log.With("user_id", 42).Warn("quota almost reached", "used", 0.95)
//	log.Error("charge failed", logger.Err(err), "order_id", id)
//
// Configuration can also come entirely from the environment:
//
//	log := logger.MustNew(logger.WithConfig(logger.ConfigFromEnv()))
//
// See the README for the full guide.
package logger
