// Command gin-api is a starter REST service: Gin on net/http with a pgx
// connection pool, graceful shutdown and request-scoped logging.
//
//	docker run -d --name pg -e POSTGRES_PASSWORD=pg -p 5432:5432 postgres:17
//	export DATABASE_URL=postgres://postgres:pg@localhost:5432/postgres
//	LOG_FORMAT=console LOG_LEVEL=debug go run .
//
//	curl localhost:8080/users/7
//	curl localhost:8080/users/abc          # 400, logged at WARN
//	curl -H 'X-Request-ID: abc' localhost:8080/users/7
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/techpartners-asia/logger-go"
	ginlog "github.com/techpartners-asia/logger-go/adapters/gin"
	pgxlog "github.com/techpartners-asia/logger-go/adapters/pgx"
)

var version = "dev"

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	log := logger.MustNew(
		logger.WithService("users-api"),
		logger.WithVersion(version),
		logger.WithRedaction(),
		logger.WithConfig(logger.ConfigFromEnv()),
	)
	defer log.Close()
	logger.SetDefault(log)
	logger.RedirectStdLog(log, logger.LevelInfo) // http.Server internals go through the logger too

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("parse DATABASE_URL", err)
	}
	cfg.ConnConfig.Tracer = pgxlog.NewTracer(log, pgxlog.Config{SlowThreshold: 200 * time.Millisecond})
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatal("connect database", err)
	}
	defer pool.Close()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(ginlog.New(log, ginlog.Config{SkipPaths: []string{"/healthz"}}), ginlog.Recovery(log))

	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })

	r.GET("/users/:id", func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "id must be a number"})
			return
		}
		var u User
		// The request context carries request_id into the query log.
		err = pool.QueryRow(c.Request.Context(), "select $1::int, 'user-' || $1::text", id).Scan(&u.ID, &u.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if err != nil {
			_ = c.Error(err) // logged under "error" on the request line
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		ginlog.FromCtx(c).Debug("user loaded", "user_id", u.ID)
		c.JSON(http.StatusOK, u)
	})

	srv := &http.Server{Addr: ":" + getenv("PORT", "8080"), Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server stopped", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
