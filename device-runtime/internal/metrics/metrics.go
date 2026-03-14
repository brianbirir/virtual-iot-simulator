// Package metrics exposes Prometheus metrics for the device runtime.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// DevicesActive is the number of currently running virtual devices.
	DevicesActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sim_devices_active",
			Help: "Number of currently running virtual devices.",
		},
		[]string{"device_type", "protocol"},
	)

	// MessagesSentTotal counts telemetry messages published (success or error).
	MessagesSentTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sim_messages_sent_total",
			Help: "Total telemetry messages published.",
		},
		[]string{"device_type", "protocol", "status"},
	)

	// PublishLatency measures end-to-end publish duration in seconds.
	PublishLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sim_publish_latency_seconds",
			Help:    "End-to-end publish duration.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"device_type", "protocol"},
	)

	// DeviceErrorsTotal counts device-level errors by category.
	DeviceErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sim_device_errors_total",
			Help: "Errors from virtual devices.",
		},
		[]string{"device_type", "error_type"},
	)

	// BackpressureDropsTotal counts TelemetryPoints dropped due to a full fan-in buffer.
	BackpressureDropsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sim_backpressure_drops_total",
			Help: "Telemetry points dropped because the fan-in channel was full.",
		},
		[]string{"device_type"},
	)

	// FaultsActiveTotal counts the number of fault injections performed.
	FaultsInjectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sim_faults_injected_total",
			Help: "Total fault injections performed.",
		},
		[]string{"fault_type"},
	)
)
