// Command fiber-api is a starter REST service: Fiber v3, GORM on Postgres and
// an outbound Resty client, all logging through one logger.
//
//	docker run -d --name pg -e POSTGRES_PASSWORD=pg -p 5432:5432 postgres:17
//	export DATABASE_URL=postgres://postgres:pg@localhost:5432/postgres
//	LOG_FORMAT=console LOG_LEVEL=debug go run .
//
//	curl -XPOST localhost:8080/orders -H 'Content-Type: application/json' \
//	     -d '{"item":"book","card_number":"4111111111111111"}'
//	curl localhost:8080/orders/1
//	curl localhost:8080/orders/999                     # 404, logged at WARN
//	curl -XPUT 'localhost:8080/admin/log-level?level=warn'
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"resty.dev/v3"

	"github.com/techpartners-asia/logger-go"
	fiberlog "github.com/techpartners-asia/logger-go/adapters/fiber"
	gormlog "github.com/techpartners-asia/logger-go/adapters/gorm"
	restylog "github.com/techpartners-asia/logger-go/adapters/resty"
)

// Set at build time: go build -ldflags "-X main.version=1.4.0".
var version = "dev"

type Order struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Item      string    `json:"item"`
	CreatedAt time.Time `json:"created_at"`
}

type createOrder struct {
	Item       string `json:"item"`
	CardNumber string `json:"card_number"`
}

func main() {
	log := logger.MustNew(
		logger.WithService("orders-api"),
		logger.WithVersion(version),
		logger.WithRedaction(),
		logger.WithStackTrace(logger.LevelError),
		logger.WithConfig(logger.ConfigFromEnv()), // LOG_* variables override the defaults above
	)
	defer log.Close()
	logger.SetDefault(log)

	db, err := gorm.Open(postgres.Open(os.Getenv("DATABASE_URL")), &gorm.Config{
		Logger: gormlog.New(log, gormlog.Config{SlowThreshold: 200 * time.Millisecond}),
	})
	if err != nil {
		log.Fatal("open database", err)
	}
	if err := db.AutoMigrate(&Order{}); err != nil {
		log.Fatal("migrate database", err)
	}

	payments := restylog.Install(
		resty.New().SetBaseURL(getenv("PAYMENTS_URL", "https://httpbin.org")).SetTimeout(5*time.Second),
		log, restylog.Config{Name: "payments", SlowThreshold: 2 * time.Second},
	)
	defer payments.Close()

	app := fiber.New()
	app.Use(fiberlog.New(log, fiberlog.Config{
		SkipPaths:     []string{"/healthz"},
		SlowThreshold: 500 * time.Millisecond,
		Fields: func(c fiber.Ctx, _ int, _ time.Duration) []logger.Attr {
			if t := c.Get("X-Tenant"); t != "" {
				return []logger.Attr{logger.String("tenant", t)}
			}
			return nil
		},
	}))

	app.Get("/healthz", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	app.Post("/orders", func(c fiber.Ctx) error {
		l := fiberlog.FromCtx(c) // carries request_id, method, path
		var req createOrder
		if err := c.Bind().Body(&req); err != nil || req.Item == "" {
			l.Info("invalid order", "reason", "item is required")
			return fiber.NewError(fiber.StatusBadRequest, "item is required")
		}
		l.Debug("charging card", "card_number", req.CardNumber) // printed as [REDACTED]

		// Passing the request context forwards X-Request-ID and tags the call log.
		if _, err := payments.R().SetContext(c.Context()).SetBody(map[string]string{"item": req.Item}).Post("/post"); err != nil {
			return fiber.NewError(fiber.StatusBadGateway, "payment failed")
		}

		o := Order{Item: req.Item}
		if err := db.WithContext(c.Context()).Create(&o).Error; err != nil {
			return err // plain error: logged at ERROR with status 500 and a stack
		}
		l.Info("order created", "order_id", o.ID)
		return c.Status(fiber.StatusCreated).JSON(o)
	})

	app.Get("/orders/:id", func(c fiber.Ctx) error {
		id, err := strconv.Atoi(c.Params("id"))
		if err != nil {
			return fiber.ErrBadRequest
		}
		var o Order
		err = db.WithContext(c.Context()).First(&o, id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound // logged at WARN, not ERROR
		}
		if err != nil {
			return err
		}
		return c.JSON(o)
	})

	// Change verbosity on a running service without a redeploy.
	app.Put("/admin/log-level", func(c fiber.Ctx) error {
		if err := log.SetLevelString(c.Query("level")); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		log.Warn("log level changed", "level", logger.LevelName(log.Level()))
		return c.SendStatus(fiber.StatusNoContent)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := ":" + getenv("PORT", "8080")
	go func() {
		log.Info("listening", "addr", addr)
		if err := app.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
			log.Error("server stopped", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		log.Error("shutdown", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
