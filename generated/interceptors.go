package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenBucketRateLimiter implements a per-client token bucket rate limiter.
type TokenBucketRateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	capacity float64
}

type bucket struct {
	tokens     float64
	lastRefill time.Time
}

// NewTokenBucketRateLimiter initializes a rate limiter.
func NewTokenBucketRateLimiter(rate float64, capacity float64) *TokenBucketRateLimiter {
	return &TokenBucketRateLimiter{
		buckets:  make(map[string]*bucket),
		rate:     rate,
		capacity: capacity,
	}
}

// Allow checks if a request is allowed under the rate limit for the client.
func (l *TokenBucketRateLimiter) Allow(clientID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, exists := l.buckets[clientID]
	now := time.Now()
	if !exists {
		l.buckets[clientID] = &bucket{
			tokens:     l.capacity - 1,
			lastRefill: now,
		}
		return true
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.lastRefill = now
	b.tokens = b.tokens + elapsed*l.rate
	if b.tokens > l.capacity {
		b.tokens = l.capacity
	}

	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

// UnaryRecoveryInterceptor catches panics in unary RPCs and returns an INTERNAL error code.
func UnaryRecoveryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Panic recovered in gRPC unary handler",
					slog.Any("panic", r),
					slog.String("method", info.FullMethod),
				)
				err = status.Errorf(codes.INTERNAL, "an unexpected internal server error occurred")
			}
		}()
		return handler(ctx, req)
	}
}

// UnaryAuthInterceptor validates the Authorization bearer token.
func UnaryAuthInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Errorf(codes.UNAUTHENTICATED, "missing metadata")
		}

		authHeader := md.Get("authorization")
		if len(authHeader) == 0 {
			return nil, status.Errorf(codes.UNAUTHENTICATED, "authorization token is required")
		}

		token := authHeader[0]
		if !strings.HasPrefix(token, "Bearer ") {
			return nil, status.Errorf(codes.UNAUTHENTICATED, "authorization header must start with Bearer")
		}

		jwtToken := strings.TrimPrefix(token, "Bearer ")
		parts := strings.Split(jwtToken, ".")
		if len(parts) != 3 {
			return nil, status.Errorf(codes.UNAUTHENTICATED, "invalid token format")
		}

		// Use the first part of the token as client ID for testing
		clientID := parts[0]
		newCtx := context.WithValue(ctx, "client_id", clientID)
		return handler(newCtx, req)
	}
}

// UnaryRateLimitInterceptor limits client calls per client_id to 1000 RPM.
func UnaryRateLimitInterceptor(logger *slog.Logger, limiter *TokenBucketRateLimiter) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		clientID, ok := ctx.Value("client_id").(string)
		if !ok || clientID == "" {
			clientID = "anonymous"
		}

		if !limiter.Allow(clientID) {
			logger.Warn("Rate limit exceeded for client", slog.String("client_id", clientID))
			return nil, status.Errorf(codes.RESOURCE_EXHAUSTED, "rate limit exceeded: 1000 RPM limit")
		}

		return handler(ctx, req)
	}
}

// UnaryLoggingInterceptor logs completed RPC metadata without logging sensitive payloads.
func UnaryLoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		txID := "unknown"
		type txnReq interface {
			GetTransactionId() string
		}
		if tr, ok := req.(txnReq); ok {
			txID = tr.GetTransactionId()
		}

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		st, _ := status.FromError(err)

		logger.Info("gRPC call complete",
			slog.String("method", info.FullMethod),
			slog.Duration("duration", duration),
			slog.String("code", st.Code().String()),
			slog.String("transaction_id", txID),
		)

		return resp, err
	}
}

// ChainUnaryInterceptors combines multiple interceptors into a single chain.
func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		buildChain := func(current grpc.UnaryServerInterceptor, next grpc.UnaryHandler) grpc.UnaryHandler {
			return func(currentCtx context.Context, currentReq interface{}) (interface{}, error) {
				return current(currentCtx, currentReq, info, next)
			}
		}

		chain := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			chain = buildChain(interceptors[i], chain)
		}

		return chain(ctx, req)
	}
}
