package server

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryInterceptor converts panics in gRPC handlers to INTERNAL status errors.
func RecoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("method", info.FullMethod).Msg("gRPC handler panicked")
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// StreamRecoveryInterceptor converts panics in streaming gRPC handlers to INTERNAL errors.
func StreamRecoveryInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("method", info.FullMethod).Msg("gRPC stream handler panicked")
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(srv, ss)
	}
}

// LoggingInterceptor logs method, duration, and status code for unary RPCs.
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		log.Info().
			Str("method", info.FullMethod).
			Dur("duration_ms", time.Since(start)).
			Str("code", code.String()).
			Msg("grpc")
		return resp, err
	}
}

// StreamLoggingInterceptor logs streaming RPC lifecycle.
func StreamLoggingInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		log.Info().
			Str("method", info.FullMethod).
			Dur("duration_ms", time.Since(start)).
			Str("code", code.String()).
			Msg("grpc stream")
		return err
	}
}
