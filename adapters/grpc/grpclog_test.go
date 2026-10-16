package grpclog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/techpartners-asia/logger-go"
	grpclog "github.com/techpartners-asia/logger-go/adapters/grpc"
	"github.com/techpartners-asia/logger-go/logtest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryServerInterceptor(t *testing.T) {
	log, rec := logtest.New()
	ic := grpclog.UnaryServerInterceptor(log, grpclog.Config{SkipMethods: []string{"/grpc.health.*"}})
	info := &grpc.UnaryServerInfo{FullMethod: "/orders.v1.Orders/Get"}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "rid"))
	_, err := ic(ctx, nil, info, func(ctx context.Context, req any) (any, error) {
		logger.FromContext(ctx).Info("inside")
		if grpclog.RequestID(ctx) != "rid" {
			t.Error("request id not in ctx")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	in := rec.Find(logger.LevelInfo, "inside")
	if len(in) != 1 || in[0].String("request_id") != "rid" || in[0].String("grpc_service") != "orders.v1.Orders" || in[0].String("grpc_method") != "Get" {
		t.Errorf("%+v", in)
	}
	if e := rec.Last(); e.Message != "grpc request" || e.String("grpc_code") != "OK" || e.Level != logger.LevelInfo || e.String("request_id") != "rid" || e.String("grpc_method") != "Get" {
		t.Errorf("%+v", e)
	}

	rec.Reset()
	_, _ = ic(context.Background(), nil, info, func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.Unavailable, "down")
	})
	if e := rec.Last(); e.Level != logger.LevelWarn || e.String("error") == "" {
		t.Errorf("%+v", e)
	}

	rec.Reset()
	_, _ = ic(context.Background(), nil, info, func(ctx context.Context, req any) (any, error) {
		return nil, errors.New("plain")
	})
	if e := rec.Last(); e.Level != logger.LevelError || e.String("grpc_code") != "Unknown" {
		t.Errorf("%+v", e)
	}

	rec.Reset()
	_, err = ic(context.Background(), nil, info, func(ctx context.Context, req any) (any, error) { panic("gp") })
	if status.Code(err) != codes.Internal || !rec.Has(logger.LevelError, "grpc panic") {
		t.Errorf("panic: err=%v entries=%+v", err, rec.Entries())
	}

	rec.Reset()
	_, _ = ic(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"},
		func(ctx context.Context, req any) (any, error) { return nil, nil })
	if rec.Len() != 0 {
		t.Error("health logged")
	}
}

func TestUnaryClientInterceptorPropagates(t *testing.T) {
	log, rec := logtest.New()
	ic := grpclog.UnaryClientInterceptor(log)
	ctx := logger.ContextWithFields(context.Background(), logger.String(logger.KeyRequestID, "c-1"))
	err := ic(ctx, "/svc/M", nil, nil, nil, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		md, _ := metadata.FromOutgoingContext(ctx)
		if got := md.Get("x-request-id"); len(got) != 1 || got[0] != "c-1" {
			t.Errorf("outgoing md = %v", md)
		}
		return status.Error(codes.NotFound, "nope")
	})
	if status.Code(err) != codes.NotFound {
		t.Fatal(err)
	}
	if e := rec.Last(); e.Level != logger.LevelWarn || e.String("grpc_code") != "NotFound" {
		t.Errorf("%+v", e)
	}
}
