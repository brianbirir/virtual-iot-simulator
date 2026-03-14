package device

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/generator"
	"github.com/virtual-iot-simulator/device-runtime/internal/protocol"
)

func makeDevice(telCh chan<- *simulatorv1.TelemetryPoint) *VirtualDevice {
	return NewVirtualDevice(DeviceConfig{
		ID:         "test-device-001",
		DeviceType: "temperature_sensor",
		Labels:     map[string]string{"env": "test"},
		Interval:   10 * time.Millisecond,
		Publisher:  protocol.NewConsolePublisherWriter(&bytes.Buffer{}),
		Topic:      "devices/test-device-001/telemetry",
		Generators: map[string]generator.Generator{
			"temperature": generator.NewGaussian(22.0, 1.0, 1),
			"status":      generator.NewStatic("ok"),
		},
		Clock:       NewRuntimeClock(),
		TelemetryCh: telCh,
	})
}

func TestDeviceStateTransitions(t *testing.T) {
	telCh := make(chan *simulatorv1.TelemetryPoint, 100)
	d := makeDevice(telCh)

	if d.State() != simulatorv1.DeviceState_DEVICE_STATE_IDLE {
		t.Fatalf("expected IDLE, got %v", d.State())
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Wait for RUNNING
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if d.State() == simulatorv1.DeviceState_DEVICE_STATE_RUNNING {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if d.State() != simulatorv1.DeviceState_DEVICE_STATE_RUNNING {
		t.Fatalf("expected RUNNING, got %v", d.State())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("device did not stop within timeout")
	}

	if d.State() != simulatorv1.DeviceState_DEVICE_STATE_STOPPED {
		t.Fatalf("expected STOPPED, got %v", d.State())
	}
}

func TestDeviceStopsWithinOneInterval(t *testing.T) {
	telCh := make(chan *simulatorv1.TelemetryPoint, 100)
	d := makeDevice(telCh)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Wait for running
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * d.Interval):
		t.Fatal("device did not stop within two intervals after cancel")
	}
}

func TestDevicePayloadContainsAllFields(t *testing.T) {
	telCh := make(chan *simulatorv1.TelemetryPoint, 100)
	d := makeDevice(telCh)
	ctx, cancel := context.WithCancel(context.Background())

	var buf bytes.Buffer
	d.Publisher = protocol.NewConsolePublisherWriter(&buf)

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Wait for at least one publish
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// Parse first JSON line from output (after "[topic] ")
	output := buf.String()
	if len(output) == 0 {
		t.Fatal("no output from publisher")
	}
	// Find first '[' + topic + '] ' prefix
	for i, ch := range output {
		if ch == ' ' && i > 0 {
			jsonPart := output[i+1:]
			end := len(jsonPart)
			for j, c := range jsonPart {
				if c == '\n' {
					end = j
					break
				}
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(jsonPart[:end]), &m); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if _, ok := m["device_id"]; !ok {
				t.Error("missing device_id in payload")
			}
			if _, ok := m["timestamp"]; !ok {
				t.Error("missing timestamp in payload")
			}
			if _, ok := m["temperature"]; !ok {
				t.Error("missing temperature in payload")
			}
			if _, ok := m["status"]; !ok {
				t.Error("missing status in payload")
			}
			break
		}
	}
}

func TestDeviceTimestampsMonotonic(t *testing.T) {
	telCh := make(chan *simulatorv1.TelemetryPoint, 200)
	d := makeDevice(telCh)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx) //nolint:errcheck
		close(done)
	}()

	time.Sleep(80 * time.Millisecond)
	cancel()
	// Wait for device to exit before reading the channel to avoid race on close.
	<-done

	// Drain collected points and verify monotonic order.
	var prev time.Time
	for {
		select {
		case pt := <-telCh:
			ts := pt.Timestamp.AsTime()
			if !prev.IsZero() && ts.Before(prev) {
				t.Fatalf("timestamp went backwards: %v < %v", ts, prev)
			}
			prev = ts
		default:
			return
		}
	}
}
