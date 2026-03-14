// Command runtime is the IoT device simulator runtime server.
// It accepts gRPC connections from the Python orchestrator and manages
// a fleet of virtual IoT devices.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/device"
	"github.com/virtual-iot-simulator/device-runtime/internal/protocol"
	"github.com/virtual-iot-simulator/device-runtime/internal/server"
)

func main() {
	// --- gRPC / admin flags ---
	port := flag.Int("port", 50051, "gRPC listen port")
	adminPort := flag.Int("admin-port", 8080, "Admin HTTP listen port (/healthz, /readyz, /metrics)")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "Graceful shutdown timeout")

	// --- Simulation flags ---
	masterSeed := flag.Int64("master-seed", 0, "RNG master seed (0 = random, non-zero = deterministic replay)")
	runID := flag.String("run-id", "", "Unique run identifier for log correlation and replay (auto-generated if empty)")
	backpressure := flag.String("backpressure", "drop_oldest", "Backpressure strategy: drop_oldest | slow_down")

	// --- MQTT flags ---
	mqttURL := flag.String("mqtt-url", "", "MQTT broker URL (e.g. tcp://localhost:1883); empty = console fallback")
	mqttQoS := flag.Int("mqtt-qos", 1, "MQTT QoS level (0, 1, or 2)")
	mqttPoolSize := flag.Int("mqtt-pool-size", 1, "Number of MQTT connections to maintain in the pool")

	// --- HTTP flags ---
	httpEndpoint := flag.String("http-endpoint", "", "HTTP telemetry endpoint URL; empty = console fallback")

	// --- AMQP flags ---
	amqpURL := flag.String("amqp-url", "", "AMQP broker URL (e.g. amqp://guest:guest@localhost:5672/); empty = console fallback")
	amqpExchange := flag.String("amqp-exchange", "iot.telemetry", "AMQP exchange name")

	flag.Parse()

	// --- Logging ---
	lvl, err := zerolog.ParseLevel(*logLevel)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Logger()

	// --- RunID: use provided value or auto-generate ---
	resolvedRunID := *runID
	if resolvedRunID == "" {
		buf := make([]byte, 8)
		if _, err := rand.Read(buf); err != nil {
			log.Fatal().Err(err).Msg("failed to generate run ID")
		}
		resolvedRunID = hex.EncodeToString(buf)
	}

	// --- Core components ---
	cfg := device.ManagerConfig{
		MasterSeed:           *masterSeed,
		RunID:                resolvedRunID,
		BackpressureStrategy: *backpressure,
		MQTT:                 protocol.MQTTConfig{BrokerURL: *mqttURL, QoS: byte(*mqttQoS), ConnectTimeout: 10 * time.Second, KeepAlive: 30 * time.Second, CleanSession: true, PoolSize: *mqttPoolSize},
		HTTP:                 protocol.HTTPConfig{Endpoint: *httpEndpoint, Timeout: 10 * time.Second, MaxIdleConn: 20},
		AMQP:                 protocol.AMQPConfig{URL: *amqpURL, Exchange: *amqpExchange, ExchangeType: "topic"},
	}

	mgr := device.NewManager(cfg)
	bc := server.NewBroadcaster(mgr.TelemetryCh())
	runtimeSrv := server.NewRuntimeServer(mgr, bc)

	// --- gRPC server ---
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

	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthSrv)
	healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthSrv.SetServingStatus("simulator.v1.DeviceRuntimeService", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal().Err(err).Int("port", *port).Msg("failed to bind gRPC port")
	}

	// --- Admin HTTP server (health + Prometheus metrics) ---
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint:errcheck
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready")) //nolint:errcheck
	})
	mux.Handle("/metrics", promhttp.Handler())

	adminSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", *adminPort),
		Handler: mux,
	}

	// --- Start servers ---
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

	log.Info().
		Int("grpc_port", *port).
		Int("admin_port", *adminPort).
		Int64("master_seed", *masterSeed).
		Str("run_id", resolvedRunID).
		Str("backpressure", *backpressure).
		Msg("runtime ready")

	// --- Graceful shutdown ---
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
