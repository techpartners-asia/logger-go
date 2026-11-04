// Package payments is a starter for an SDK or shared library: a client for a
// third-party HTTP API built on Resty.
//
// A library never configures logging itself. It takes a *logger.Logger from
// the caller and stays silent when none is given, so the host service decides
// format, level, redaction and where the lines go.
//
//	client := payments.New("https://api.example.com", apiKey,
//		payments.WithLogger(log), // the service's logger
//	)
//	charge, err := client.Charge(ctx, payments.ChargeRequest{OrderID: "991", Amount: 15000})
package payments

import (
	"context"
	"fmt"
	"time"

	"resty.dev/v3"

	"github.com/techpartners-asia/logger-go"
	restylog "github.com/techpartners-asia/logger-go/adapters/resty"
)

type Client struct {
	http *resty.Client
	log  *logger.Logger
}

type Option func(*Client)

// WithLogger routes the client's logs through log, named "payments".
func WithLogger(log *logger.Logger) Option {
	return func(c *Client) { c.log = log }
}

func New(baseURL, apiKey string, opts ...Option) *Client {
	c := &Client{log: logger.NewNop()}
	for _, o := range opts {
		o(c)
	}
	c.http = restylog.Install(
		resty.New().
			SetBaseURL(baseURL).
			SetHeader("X-Api-Key", apiKey).
			SetTimeout(10*time.Second).
			SetRetryCount(2),
		c.log, restylog.Config{Name: "payments", SlowThreshold: 2 * time.Second},
	)
	c.log = c.log.Named("payments")
	return c
}

func (c *Client) Close() error { return c.http.Close() }

type ChargeRequest struct {
	OrderID string `json:"order_id"`
	Amount  int64  `json:"amount"`
}

type Charge struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type APIError struct {
	Status int
	Code   string `json:"code"`
}

func (e *APIError) Error() string { return fmt.Sprintf("payments: %d %s", e.Status, e.Code) }

// Charge returns errors instead of logging them at ERROR. The caller knows
// whether a declined card is an incident or a normal day.
func (c *Client) Charge(ctx context.Context, req ChargeRequest) (*Charge, error) {
	var out Charge
	var apiErr APIError
	resp, err := c.http.R().
		SetContext(ctx). // forwards X-Request-ID and tags the call log
		SetBody(req).
		SetResult(&out).
		SetResultError(&apiErr).
		Post("/v1/charges")
	if err != nil {
		return nil, fmt.Errorf("payments: charge: %w", err)
	}
	if resp.IsStatusFailure() {
		apiErr.Status = resp.StatusCode()
		c.log.WarnContext(ctx, "charge rejected", "order_id", req.OrderID, "code", apiErr.Code)
		return nil, &apiErr
	}
	c.log.DebugContext(ctx, "charge created", "order_id", req.OrderID, "charge_id", out.ID)
	return &out, nil
}
