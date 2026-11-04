// Command grpc-service is a starter gRPC server with logging interceptors on
// both the server and the client side.
//
// It serves the standard health service so it runs without protoc. Register
// your generated service next to it.
//
//	LOG_FORMAT=console go run .                          # server on :9090
//	LOG_FORMAT=console go run . -check localhost:9090    # client call, logged on both sides
package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/techpartners-asia/logger-go"
	grpclog "github.com/techpartners-asia/logger-go/adapters/grpc"
)

var version = "dev"

func main() {
	check := flag.String("check", "", "call Health/Check on this address instead of serving")
	flag.Parse()

	log := logger.MustNew(
		logger.WithService("billing-grpc"),
		logger.WithVersion(version),
		logger.WithRedaction(),
		logger.WithConfig(logger.ConfigFromEnv()),
	)
	defer log.Close()
	logger.SetDefault(log)

	if *check != "" {
		runClient(log, *check)
		return
	}
	runServer(log)
}

func runServer(log *logger.Logger) {
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpclog.UnaryServerInterceptor(log, grpclog.Config{
			SlowThreshold: 300 * time.Millisecond,
		})),
		grpc.ChainStreamInterceptor(grpclog.StreamServerInterceptor(log)),
	)

	hs := health.NewServer()
	hs.SetServingStatus("billing", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, hs)
	// pb.RegisterBillingServer(srv, &billingServer{log: log.Named("billing")})
	//
	// Inside a handler, logger.FromContext(ctx) returns a logger that already
	// carries request_id, grpc_service and grpc_method.

	addr := ":" + getenv("PORT", "9090")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal("listen", err, "addr", addr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		hs.Shutdown()
		srv.GracefulStop()
	}()

	log.Info("listening", "addr", addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatal("serve", err)
	}
}

func runClient(log *logger.Logger, addr string) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpclog.UnaryClientInterceptor(log)),
		grpc.WithStreamInterceptor(grpclog.StreamClientInterceptor()),
	)
	if err != nil {
		log.Fatal("dial", err, "addr", addr)
	}
	defer conn.Close()

	// The request_id in the context is sent as x-request-id metadata, so the
	// server's log line carries the same id.
	ctx := logger.ContextWith(context.Background(), logger.KeyRequestID, "cli-check-1")
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: "billing"})
	if err != nil {
		log.Fatal("health check failed", err)
	}
	log.InfoContext(ctx, "health check", "status", resp.GetStatus().String())
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
