// Package grpclog provides gRPC interceptors that log each RPC, propagate
// request IDs through metadata, and put a request-scoped logger into the
// context.
//
//	srv := grpc.NewServer(
//		grpc.ChainUnaryInterceptor(grpclog.UnaryServerInterceptor(log)),
//		grpc.ChainStreamInterceptor(grpclog.StreamServerInterceptor(log)),
//	)
//
//	conn, _ := grpc.NewClient(addr,
//		grpc.WithUnaryInterceptor(grpclog.UnaryClientInterceptor(log)),
//	)
package grpclog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"runtime/debug"
	"strings"
	"time"

	"github.com/techpartners-asia/logger-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// RequestIDKey is the metadata key used for correlation.
const RequestIDKey = "x-request-id"

// Config tunes the server interceptors.
type Config struct {
	// SkipMethods are full method names ("/pkg.Service/Method") not to log.
	// Prefix match when ending with "*".
	SkipMethods []string
	// SlowThreshold logs slower RPCs at WARN.
	SlowThreshold time.Duration
	// Message overrides the log text. Default "grpc request".
	Message string
	// Fields appends attributes after the handler ran.
	Fields func(ctx context.Context, method string, code codes.Code, duration time.Duration) []logger.Attr
	// LevelFor overrides the code-to-level mapping.
	LevelFor func(code codes.Code) logger.Level
	// DisableRecover turns off panic recovery (panics become codes.Internal).
	DisableRecover bool
	GenerateID     func() string
}

func (c *Config) defaults() {
	if c.Message == "" {
		c.Message = "grpc request"
	}
	if c.LevelFor == nil {
		c.LevelFor = DefaultLevelFor
	}
	if c.GenerateID == nil {
		c.GenerateID = newRequestID
	}
}

// DefaultLevelFor maps gRPC codes to log levels: OK and client-caused codes
// are INFO, retryable/expected ones WARN, server faults ERROR.
func DefaultLevelFor(code codes.Code) logger.Level {
	switch code {
	case codes.OK, codes.Canceled, codes.InvalidArgument, codes.NotFound,
		codes.AlreadyExists, codes.Unauthenticated:
		return logger.LevelInfo
	case codes.DeadlineExceeded, codes.PermissionDenied, codes.ResourceExhausted,
		codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unavailable:
		return logger.LevelWarn
	default:
		return logger.LevelError
	}
}

// UnaryServerInterceptor logs unary RPCs.
func UnaryServerInterceptor(log *logger.Logger, cfgs ...Config) grpc.UnaryServerInterceptor {
	cfg := first(cfgs)
	cfg.defaults()
	if log == nil {
		log = logger.Default()
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		if skip(cfg.SkipMethods, info.FullMethod) {
			return handler(ctx, req)
		}
		start := time.Now()
		ctx, reqLog := prepare(ctx, log, info.FullMethod, cfg)
		defer func() {
			if !cfg.DisableRecover {
				if r := recover(); r != nil {
					reqLog.LogAttrs(ctx, logger.LevelError, "grpc panic",
						logger.Any("panic", r), logger.String(logger.KeyStack, string(debug.Stack())))
					err = status.Error(codes.Internal, "internal error")
				}
			}
			finish(ctx, log, cfg, info.FullMethod, "unary", start, err)
		}()
		return handler(ctx, req)
	}
}

// StreamServerInterceptor logs streaming RPCs when the stream ends.
func StreamServerInterceptor(log *logger.Logger, cfgs ...Config) grpc.StreamServerInterceptor {
	cfg := first(cfgs)
	cfg.defaults()
	if log == nil {
		log = logger.Default()
	}
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		if skip(cfg.SkipMethods, info.FullMethod) {
			return handler(srv, ss)
		}
		start := time.Now()
		ctx, reqLog := prepare(ss.Context(), log, info.FullMethod, cfg)
		ws := &wrappedStream{ServerStream: ss, ctx: ctx}
		defer func() {
			if !cfg.DisableRecover {
				if r := recover(); r != nil {
					reqLog.LogAttrs(ctx, logger.LevelError, "grpc panic",
						logger.Any("panic", r), logger.String(logger.KeyStack, string(debug.Stack())))
					err = status.Error(codes.Internal, "internal error")
				}
			}
			finish(ctx, log, cfg, info.FullMethod, "stream", start, err)
		}()
		return handler(srv, ws)
	}
}

// UnaryClientInterceptor logs outgoing unary RPCs at DEBUG (WARN/ERROR on
// failure) and forwards the request ID from the context as metadata.
func UnaryClientInterceptor(log *logger.Logger) grpc.UnaryClientInterceptor {
	if log == nil {
		log = logger.Default()
	}
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = propagate(ctx)
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		code := status.Code(err)
		attrs := []logger.Attr{
			logger.String("grpc_method", method),
			logger.String("grpc_code", code.String()),
			logger.Duration(time.Since(start)),
		}
		level := logger.LevelDebug
		if err != nil {
			attrs = append(attrs, logger.Err(err))
			level = DefaultLevelFor(code)
			if level < logger.LevelWarn {
				level = logger.LevelWarn
			}
		}
		log.LogAttrs(ctx, level, "grpc client request", attrs...)
		return err
	}
}

// StreamClientInterceptor forwards the request ID for client streams.
func StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(propagate(ctx), desc, cc, method, opts...)
	}
}

// RequestID returns the request ID attached to ctx by the server interceptor.
func RequestID(ctx context.Context) string {
	for _, a := range logger.FieldsFromContext(ctx) {
		if a.Key == logger.KeyRequestID {
			return a.Value.String()
		}
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(RequestIDKey); len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// --- internals -------------------------------------------------------------

func prepare(ctx context.Context, log *logger.Logger, method string, cfg Config) (context.Context, *logger.Logger) {
	reqID := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(RequestIDKey); len(v) > 0 {
			reqID = v[0]
		}
	}
	if reqID == "" {
		reqID = cfg.GenerateID()
	}
	_ = grpc.SetHeader(ctx, metadata.Pairs(RequestIDKey, reqID))

	svc, m := splitMethod(method)
	reqLog := log.WithAttrs(
		logger.String(logger.KeyRequestID, reqID),
		logger.String("grpc_service", svc),
		logger.String("grpc_method", m),
	)
	ctx = logger.NewContext(ctx, reqLog)
	ctx = logger.ContextWithFields(ctx, logger.String(logger.KeyRequestID, reqID))
	return ctx, reqLog
}

// finish emits the completion line via the base logger: request_id arrives
// through the context fields, so it is not duplicated by reqLog's attrs.
func finish(ctx context.Context, log *logger.Logger, cfg Config, method, kind string, start time.Time, err error) {
	dur := time.Since(start)
	code := status.Code(err)
	svc, m := splitMethod(method)
	attrs := []logger.Attr{
		logger.String("grpc_service", svc),
		logger.String("grpc_method", m),
		logger.String("grpc_type", kind),
		logger.String("grpc_code", code.String()),
		logger.Duration(dur),
	}
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		attrs = append(attrs, logger.String(logger.KeyRemoteAddr, p.Addr.String()))
	}
	if err != nil {
		attrs = append(attrs, logger.Err(err))
	}
	if cfg.Fields != nil {
		attrs = append(attrs, cfg.Fields(ctx, method, code, dur)...)
	}
	level := cfg.LevelFor(code)
	if cfg.SlowThreshold > 0 && dur > cfg.SlowThreshold && level < logger.LevelWarn {
		level = logger.LevelWarn
		attrs = append(attrs, logger.Bool("slow", true))
	}
	log.LogAttrs(ctx, level, cfg.Message, attrs...)
}

func propagate(ctx context.Context) context.Context {
	if id := RequestID(ctx); id != "" {
		if md, ok := metadata.FromOutgoingContext(ctx); !ok || len(md.Get(RequestIDKey)) == 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, RequestIDKey, id)
		}
	}
	return ctx
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }

func splitMethod(full string) (service, method string) {
	full = strings.TrimPrefix(full, "/")
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[:i], full[i+1:]
	}
	return "", full
}

func skip(skips []string, method string) bool {
	for _, s := range skips {
		if strings.HasSuffix(s, "*") {
			if strings.HasPrefix(method, strings.TrimSuffix(s, "*")) {
				return true
			}
		} else if method == s {
			return true
		}
	}
	return false
}

func first(cfgs []Config) Config {
	if len(cfgs) > 0 {
		return cfgs[0]
	}
	return Config{}
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
