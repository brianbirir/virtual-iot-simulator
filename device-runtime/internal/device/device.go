package device

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/generator"
	"github.com/virtual-iot-simulator/device-runtime/internal/metrics"
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
	Protocol   string
	Labels     map[string]string
	Interval   time.Duration
	Publisher  protocol.Publisher
	Topic      string

	// telemetryCh is a write-only reference to the Manager's fan-in channel.
	telemetryCh chan<- *simulatorv1.TelemetryPoint

	// eventsCh delivers lifecycle events to the Manager's StreamEvents broadcaster.
	eventsCh chan<- *simulatorv1.DeviceEvent

	generators map[string]generator.Generator
	clock      *RuntimeClock
	state      simulatorv1.DeviceState
	cancel     context.CancelFunc
	mu         sync.RWMutex // protects state, generators, cancel

	faults   []ActiveFault
	faultsMu sync.RWMutex
}

// DeviceConfig holds constructor parameters for a VirtualDevice.
type DeviceConfig struct {
	ID          string
	DeviceType  string
	Protocol    string
	Labels      map[string]string
	Interval    time.Duration
	Publisher   protocol.Publisher
	Topic       string
	Generators  map[string]generator.Generator
	Clock       *RuntimeClock
	TelemetryCh chan<- *simulatorv1.TelemetryPoint
	EventsCh    chan<- *simulatorv1.DeviceEvent
}

// NewVirtualDevice constructs a VirtualDevice in the IDLE state.
func NewVirtualDevice(cfg DeviceConfig) *VirtualDevice {
	return &VirtualDevice{
		ID:          cfg.ID,
		DeviceType:  cfg.DeviceType,
		Protocol:    cfg.Protocol,
		Labels:      cfg.Labels,
		Interval:    cfg.Interval,
		Publisher:   cfg.Publisher,
		Topic:       cfg.Topic,
		generators:  cfg.Generators,
		clock:       cfg.Clock,
		telemetryCh: cfg.TelemetryCh,
		eventsCh:    cfg.EventsCh,
		state:       simulatorv1.DeviceState_DEVICE_STATE_IDLE,
	}
}

// State returns the current device state (thread-safe).
func (d *VirtualDevice) State() simulatorv1.DeviceState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

// setState updates the device state (must NOT hold mu — acquires it internally).
func (d *VirtualDevice) setState(s simulatorv1.DeviceState) {
	d.mu.Lock()
	d.state = s
	d.mu.Unlock()
}

// AddFault appends a fault to the device's active fault list.
func (d *VirtualDevice) AddFault(f ActiveFault) {
	d.faultsMu.Lock()
	defer d.faultsMu.Unlock()
	d.faults = append(d.faults, f)
}

// UpdateGenerators atomically replaces the generator map (used by UpdateDeviceBehavior).
func (d *VirtualDevice) UpdateGenerators(gens map[string]generator.Generator) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.generators = gens
}

// Run blocks until ctx is cancelled. It should be called in a goroutine.
func (d *VirtualDevice) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.cancel = cancel
	d.mu.Unlock()
	defer cancel()

	d.setState(simulatorv1.DeviceState_DEVICE_STATE_RUNNING)
	metrics.DevicesActive.WithLabelValues(d.DeviceType, d.Protocol).Inc()
	log.Info().Str("device_id", d.ID).Str("device_type", d.DeviceType).Msg("device spawned")
	d.emitEvent(simulatorv1.DeviceEventType_DEVICE_EVENT_TYPE_SPAWNED, "device started")

	ticker := time.NewTicker(d.Interval)
	defer ticker.Stop()
	defer func() {
		d.setState(simulatorv1.DeviceState_DEVICE_STATE_STOPPED)
		metrics.DevicesActive.WithLabelValues(d.DeviceType, d.Protocol).Dec()
		log.Info().Str("device_id", d.ID).Msg("device stopped")
		d.emitEvent(simulatorv1.DeviceEventType_DEVICE_EVENT_TYPE_STOPPED, "device stopped")
	}()

	devState := map[string]any{}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			now := d.clock.Now()
			payload, points := d.generatePayload(devState, now)

			// Apply active faults — may modify payload, points, or suppress publish.
			payload, points, shouldPublish := d.applyFaults(payload, points, now)

			// Fan-in: non-blocking send to broadcaster channel.
			for _, pt := range points {
				select {
				case d.telemetryCh <- pt:
				default:
					metrics.BackpressureDropsTotal.WithLabelValues(d.DeviceType).Inc()
				}
			}

			if !shouldPublish {
				continue
			}

			start := time.Now()
			if err := d.Publisher.Publish(ctx, d.Topic, payload); err != nil {
				log.Warn().Str("device_id", d.ID).Err(err).Msg("publish error")
				metrics.MessagesSentTotal.WithLabelValues(d.DeviceType, d.Protocol, "error").Inc()
				metrics.DeviceErrorsTotal.WithLabelValues(d.DeviceType, "publish_error").Inc()
			} else {
				metrics.MessagesSentTotal.WithLabelValues(d.DeviceType, d.Protocol, "ok").Inc()
				metrics.PublishLatency.WithLabelValues(d.DeviceType, d.Protocol).Observe(time.Since(start).Seconds())
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

// telemetryPayload is the structured JSON envelope published to brokers.
type telemetryPayload struct {
	DeviceID   string            `json:"device_id"`
	DeviceType string            `json:"device_type"`
	Timestamp  string            `json:"timestamp"`
	Fields     map[string]any    `json:"fields"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// generatePayload iterates generators and returns a structured JSON byte slice
// and a slice of TelemetryPoints for the internal fan-in channel.
func (d *VirtualDevice) generatePayload(devState map[string]any, now time.Time) ([]byte, []*simulatorv1.TelemetryPoint) {
	ts := timestamppb.New(now)

	d.mu.RLock()
	gens := d.generators
	d.mu.RUnlock()

	fields := make(map[string]any, len(gens))
	points := make([]*simulatorv1.TelemetryPoint, 0, len(gens))

	for name, gen := range gens {
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

	envelope := telemetryPayload{
		DeviceID:   d.ID,
		DeviceType: d.DeviceType,
		Timestamp:  now.Format(time.RFC3339Nano),
		Fields:     fields,
		Labels:     d.Labels,
	}
	payload, _ := json.Marshal(envelope)
	return payload, points
}

// applyFaults applies all active (non-expired) faults to the payload and points.
// Returns the (possibly modified) payload, points, and whether to publish.
func (d *VirtualDevice) applyFaults(
	payload []byte,
	points []*simulatorv1.TelemetryPoint,
	now time.Time,
) ([]byte, []*simulatorv1.TelemetryPoint, bool) {
	d.faultsMu.Lock()
	defer d.faultsMu.Unlock()

	// Lazily remove expired faults.
	active := d.faults[:0]
	for _, f := range d.faults {
		if !f.IsExpired(now) {
			active = append(active, f)
		}
	}
	d.faults = active

	shouldPublish := true
	for _, f := range d.faults {
		switch f.Type {
		case simulatorv1.FaultType_FAULT_TYPE_DISCONNECT:
			shouldPublish = false

		case simulatorv1.FaultType_FAULT_TYPE_LATENCY_SPIKE:
			if ms, ok := f.Params["latency_ms"].(float64); ok && ms > 0 {
				time.Sleep(time.Duration(ms) * time.Millisecond)
			}

		case simulatorv1.FaultType_FAULT_TYPE_DATA_CORRUPTION:
			rate := 1.0
			if r, ok := f.Params["corruption_rate"].(float64); ok {
				rate = r
			}
			payload = corruptPayload(payload, rate)

		case simulatorv1.FaultType_FAULT_TYPE_CLOCK_DRIFT:
			driftRate := 0.0
			if r, ok := f.Params["drift_rate_ms_per_sec"].(float64); ok {
				driftRate = r
			}
			elapsed := now.Sub(f.StartedAt).Seconds()
			offsetMs := driftRate * elapsed
			offset := time.Duration(offsetMs) * time.Millisecond
			driftedTS := timestamppb.New(now.Add(offset))
			for _, pt := range points {
				pt.Timestamp = driftedTS
			}

		case simulatorv1.FaultType_FAULT_TYPE_BATTERY_DRAIN:
			multiplier := 10.0
			if m, ok := f.Params["drain_multiplier"].(float64); ok {
				multiplier = m
			}
			for _, pt := range points {
				if pt.MetricName == "battery" {
					if dv, ok := pt.Value.(*simulatorv1.TelemetryPoint_DoubleValue); ok {
						drained := dv.DoubleValue - multiplier*(now.Sub(f.StartedAt).Seconds()/3600.0)
						if drained < 0 {
							drained = 0
						}
						pt.Value = &simulatorv1.TelemetryPoint_DoubleValue{DoubleValue: drained}
					}
				}
			}
		}
	}
	return payload, points, shouldPublish
}

// corruptPayload randomly corrupts payload bytes at the given rate (0.0–1.0).
func corruptPayload(payload []byte, rate float64) []byte {
	if len(payload) == 0 {
		return payload
	}
	corrupted := make([]byte, len(payload))
	copy(corrupted, payload)
	// Simple corruption: replace a fraction of bytes with 0x00.
	n := int(float64(len(corrupted)) * rate)
	for i := 0; i < n; i++ {
		corrupted[i] = 0x00
	}
	return corrupted
}

// emitEvent sends a DeviceEvent to the events channel (non-blocking).
func (d *VirtualDevice) emitEvent(eventType simulatorv1.DeviceEventType, msg string) {
	if d.eventsCh == nil {
		return
	}
	evt := &simulatorv1.DeviceEvent{
		DeviceId:  d.ID,
		EventType: eventType,
		Message:   msg,
		Timestamp: timestamppb.Now(),
		Metadata:  map[string]string{"device_type": d.DeviceType},
	}
	select {
	case d.eventsCh <- evt:
	default: // drop if buffer full
	}
}
