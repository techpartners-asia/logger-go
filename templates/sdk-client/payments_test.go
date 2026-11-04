package payments_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/techpartners-asia/logger-go"
	"github.com/techpartners-asia/logger-go/logtest"

	"example.com/payments"
)

func TestChargeLogsAndForwardsRequestID(t *testing.T) {
	var gotRequestID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/charges" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"id":"ch_1","status":"succeeded"}`))
	}))
	defer srv.Close()

	log, rec := logtest.New()
	client := payments.New(srv.URL, "key", payments.WithLogger(log))
	defer client.Close()

	ctx := logger.ContextWith(context.Background(), logger.KeyRequestID, "req-42")
	charge, err := client.Charge(ctx, payments.ChargeRequest{OrderID: "991", Amount: 15000})
	if err != nil {
		t.Fatal(err)
	}
	if charge.ID != "ch_1" {
		t.Errorf("charge id = %q", charge.ID)
	}
	if gotRequestID != "req-42" {
		t.Errorf("X-Request-ID = %q, want req-42", gotRequestID)
	}
	created := rec.Find(logger.LevelDebug, "charge created")
	if len(created) != 1 || created[0].Attrs["charge_id"] != "ch_1" || created[0].Attrs["request_id"] != "req-42" {
		t.Errorf("charge created log = %v, all: %v", created, rec.Messages())
	}
}

func TestChargeRejectedReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"code":"card_declined"}`))
	}))
	defer srv.Close()

	log, rec := logtest.New()
	client := payments.New(srv.URL, "key", payments.WithLogger(log))
	defer client.Close()

	_, err := client.Charge(context.Background(), payments.ChargeRequest{OrderID: "991"})
	var apiErr *payments.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 402 || apiErr.Code != "card_declined" {
		t.Fatalf("err = %v", err)
	}
	if !rec.Has(logger.LevelWarn, "charge rejected") {
		t.Errorf("want charge rejected warning, got %v", rec.Messages())
	}
	if len(rec.Find(logger.LevelError, "charge rejected")) != 0 {
		t.Error("a rejected charge must not log at ERROR")
	}
}

func TestSilentWithoutLogger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := payments.New(srv.URL, "key")
	defer client.Close()
	if _, err := client.Charge(context.Background(), payments.ChargeRequest{}); err != nil {
		t.Fatal(err)
	}
}
