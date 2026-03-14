// Command runtime is the IoT device simulator runtime server.
// It accepts gRPC connections from the Python orchestrator and manages
// a fleet of virtual IoT devices.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/device"
	"github.com/virtual-iot-simulator/device-runtime/internal/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	port := flag.Int("port", 50051, "gRPC listen port")
	adminPort := flag.Int("admin-port", 8080, "Admin HTTP listen port (/healthz, /readyz, /metrics)")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "Graceful shutdown timeout")
	flag.Parse()

	// Configure zerolog
	lvl, err := zerolog.ParseLevel(*logLevel)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Logger()

	// Core components
	mgr := device.NewManager()
	bc := server.NewBroadcaster(mgr.TelemetryCh())
	runtimeSrv := server.NewRuntimeServer(mgr, bc)

	// gRPC server
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			server.RecoveryInterceptor(),
			server.LoggingInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			server.StreamRecoveryInterceptor(),
			server.StreamLoggingInterceptor(),
		),
	)
	simulatorv1.RegisterDeviceRuntimeServiceServer(grpcServer, runtimeSrv)

	// Health check
	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthSrv)
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthSrv.SetServingStatus("simulator.v1.DeviceRuntimeService", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal().Err(err).Int("port", *port).Msg("failed to bind gRPC port")
	}

	// Admin HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint:errcheck
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready")) //nolint:errcheck
	})
	adminSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", *adminPort),
		Handler: mux,
	}

	// Start servers
	go func() {
		log.Info().Int("port", *adminPort).Msg("admin HTTP server starting")
		if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("admin HTTP server error")
		}
	}()

	go func() {
		log.Info().Int("port", *port).Msg("gRPC server starting")
		if err := grpcServer.Serve(lis); err != nil {
			log.Error().Err(err).Msg("gRPC server error")
		}
	}()

	log.Info().Int("grpc_port", *port).Int("admin_port", *adminPort).Msg("runtime ready")

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Dur("timeout", *shutdownTimeout).Msg("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer cancel()

	mgr.Shutdown()
	bc.Stop()
	grpcServer.GracefulStop()

	if err := adminSrv.Shutdown(ctx); err != nil {
		log.Warn().Err(err).Msg("admin server shutdown error")
	}

	log.Info().Msg("shutdown complete")
}
