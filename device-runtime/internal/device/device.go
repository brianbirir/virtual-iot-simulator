package device

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/generator"
	"github.com/virtual-iot-simulator/device-runtime/internal/protocol"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// VirtualDevice simulates a single IoT device. Run in a goroutine via Run().
//
// DeviceState uses the proto-generated simulatorv1.DeviceState enum directly —
// no local alias is used so the type is consistent across gRPC boundaries.
type VirtualDevice struct {
	ID         string
	DeviceType string
	Labels     map[string]string
	Interval   time.Duration
	Publisher  protocol.Publisher
	Topic      string

	// telemetryCh is a write-only reference to the Manager's fan-in channel.
	// Each generated TelemetryPoint is sent here so StreamTelemetry RPCs can
	// observe all devices without intercepting the external Publisher.
	telemetryCh chan<- *simulatorv1.TelemetryPoint

	generators map[string]generator.Generator // field_name → generator
	clock      *RuntimeClock
	state      simulatorv1.DeviceState
	cancel     context.CancelFunc
	mu         sync.RWMutex // protects state
}

// DeviceConfig holds constructor parameters for a VirtualDevice.
type DeviceConfig struct {
	ID          string
	DeviceType  string
	Labels      map[string]string
	Interval    time.Duration
	Publisher   protocol.Publisher
	Topic       string
	Generators  map[string]generator.Generator
	Clock       *RuntimeClock
	TelemetryCh chan<- *simulatorv1.TelemetryPoint
}

// NewVirtualDevice constructs a VirtualDevice in the IDLE state.
func NewVirtualDevice(cfg DeviceConfig) *VirtualDevice {
	return &VirtualDevice{
		ID:          cfg.ID,
		DeviceType:  cfg.DeviceType,
		Labels:      cfg.Labels,
		Interval:    cfg.Interval,
		Publisher:   cfg.Publisher,
		Topic:       cfg.Topic,
		generators:  cfg.Generators,
		clock:       cfg.Clock,
		telemetryCh: cfg.TelemetryCh,
		state:       simulatorv1.DeviceState_DEVICE_STATE_IDLE,
	}
}

// State returns the current device state (thread-safe).
func (d *VirtualDevice) State() simulatorv1.DeviceState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

// setState updates the device state (must hold mu).
func (d *VirtualDevice) setState(s simulatorv1.DeviceState) {
	d.mu.Lock()
	d.state = s
	d.mu.Unlock()
}

// Run blocks until ctx is cancelled. It should be called in a goroutine.
func (d *VirtualDevice) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.cancel = cancel
	d.mu.Unlock()
	defer cancel()

	d.setState(simulatorv1.DeviceState_DEVICE_STATE_RUNNING)
	log.Info().Str("device_id", d.ID).Str("device_type", d.DeviceType).Msg("device spawned")

	ticker := time.NewTicker(d.Interval)
	defer ticker.Stop()
	defer func() {
		d.setState(simulatorv1.DeviceState_DEVICE_STATE_STOPPED)
		log.Info().Str("device_id", d.ID).Msg("device stopped")
	}()

	devState := map[string]any{}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-ticker.C:
			_ = t // ticker time; use clock for sim-time
			payload, points := d.generatePayload(devState)

			// Fan-in: non-blocking send to broadcaster channel (drop if full).
			for _, pt := range points {
				select {
				case d.telemetryCh <- pt:
				default: // back-pressure drop; increment metric in Phase 5
				}
			}

			if err := d.Publisher.Publish(ctx, d.Topic, payload); err != nil {
				log.Warn().Str("device_id", d.ID).Err(err).Msg("publish error")
			}
		}
	}
}

// Stop cancels the device's context, causing Run to return.
func (d *VirtualDevice) Stop() {
	d.mu.RLock()
	cancel := d.cancel
	d.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// generatePayload iterates generators, returns a JSON byte slice and a slice of
// TelemetryPoints (one per field) for the internal fan-in channel.
func (d *VirtualDevice) generatePayload(devState map[string]any) ([]byte, []*simulatorv1.TelemetryPoint) {
	now := d.clock.Now()
	ts := timestamppb.New(now)

	fields := map[string]any{
		"device_id": d.ID,
		"timestamp": now.Format(time.RFC3339Nano),
	}

	points := make([]*simulatorv1.TelemetryPoint, 0, len(d.generators))

	for name, gen := range d.generators {
		val := gen.Next(now, devState)
		fields[name] = val

		pt := &simulatorv1.TelemetryPoint{
			DeviceId:   d.ID,
			MetricName: name,
			Timestamp:  ts,
			Tags:       d.Labels,
		}
		switch v := val.(type) {
		case float64:
			pt.Value = &simulatorv1.TelemetryPoint_DoubleValue{DoubleValue: v}
		case int64:
			pt.Value = &simulatorv1.TelemetryPoint_IntValue{IntValue: v}
		case string:
			pt.Value = &simulatorv1.TelemetryPoint_StringValue{StringValue: v}
		case bool:
			pt.Value = &simulatorv1.TelemetryPoint_BoolValue{BoolValue: v}
		}
		points = append(points, pt)
	}

	payload, _ := json.Marshal(fields)
	return payload, points
}
