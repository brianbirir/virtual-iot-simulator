package device

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/generator"
	"github.com/virtual-iot-simulator/device-runtime/internal/protocol"
	"google.golang.org/protobuf/types/known/durationpb"
)

const defaultTelemetryBufferSize = 10_000

// Manager owns the fleet lifecycle: spawning, stopping, and querying devices.
type Manager struct {
	devices     map[string]*VirtualDevice
	mu          sync.RWMutex
	clock       *RuntimeClock
	telemetryCh chan *simulatorv1.TelemetryPoint
	// ctx is the parent context; cancelling it stops all devices.
	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a Manager with a shared RuntimeClock and telemetry fan-in channel.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		devices:     make(map[string]*VirtualDevice),
		clock:       NewRuntimeClock(),
		telemetryCh: make(chan *simulatorv1.TelemetryPoint, defaultTelemetryBufferSize),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// TelemetryCh returns the read side of the fan-in telemetry channel.
func (m *Manager) TelemetryCh() <-chan *simulatorv1.TelemetryPoint {
	return m.telemetryCh
}

// Clock returns the shared simulation clock.
func (m *Manager) Clock() *RuntimeClock {
	return m.clock
}

// Spawn spawns devices from the provided specs. Returns the count of successfully
// spawned devices and a map of device_id → failure reason for any that failed.
func (m *Manager) Spawn(specs []*simulatorv1.DeviceSpec) (int, map[string]string) {
	failures := map[string]string{}
	spawned := 0

	for _, spec := range specs {
		if err := m.spawnOne(spec); err != nil {
			failures[spec.DeviceId] = err.Error()
			log.Warn().Str("device_id", spec.DeviceId).Err(err).Msg("spawn failed")
		} else {
			spawned++
		}
	}
	return spawned, failures
}

// spawnOne creates and launches a single device.
func (m *Manager) spawnOne(spec *simulatorv1.DeviceSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.devices[spec.DeviceId]; exists {
		return fmt.Errorf("device %q already exists", spec.DeviceId)
	}

	interval := 5 * time.Second
	if spec.TelemetryInterval != nil {
		interval = durationFromProto(spec.TelemetryInterval)
	}

	pub := m.publisherForProtocol(spec.Protocol)

	topic := spec.TopicTemplate
	if topic == "" {
		topic = fmt.Sprintf("devices/%s/telemetry", spec.DeviceId)
	} else {
		topic = strings.ReplaceAll(topic, "{device_id}", spec.DeviceId)
	}

	gens, err := m.buildGenerators(spec)
	if err != nil {
		return fmt.Errorf("building generators: %w", err)
	}

	d := NewVirtualDevice(DeviceConfig{
		ID:          spec.DeviceId,
		DeviceType:  spec.DeviceType,
		Labels:      spec.Labels,
		Interval:    interval,
		Publisher:   pub,
		Topic:       topic,
		Generators:  gens,
		Clock:       m.clock,
		TelemetryCh: m.telemetryCh,
	})

	m.devices[spec.DeviceId] = d
	go func() {
		if err := d.Run(m.ctx); err != nil && m.ctx.Err() == nil {
			log.Error().Str("device_id", spec.DeviceId).Err(err).Msg("device run error")
		}
	}()

	return nil
}

// Stop stops devices matching the selector. Returns stopped count and failure map.
func (m *Manager) Stop(selector *simulatorv1.DeviceSelector, graceful bool) (int, map[string]string) {
	ids := m.resolveSelector(selector)
	failures := map[string]string{}
	stopped := 0

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		d, ok := m.devices[id]
		if !ok {
			failures[id] = "device not found"
			continue
		}
		d.Stop()
		delete(m.devices, id)
		stopped++
	}
	return stopped, failures
}

// GetStatus returns fleet status for devices matching selector.
// A nil selector returns all devices.
func (m *Manager) GetStatus(selector *simulatorv1.DeviceSelector) *simulatorv1.FleetStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var profiles []*simulatorv1.DeviceProfile
	byState := map[string]int32{}
	byType := map[string]int32{}

	for id, d := range m.devices {
		if selector != nil && !m.matchesSelector(id, d, selector) {
			continue
		}
		state := d.State()
		p := &simulatorv1.DeviceProfile{
			DeviceId:   d.ID,
			DeviceType: d.DeviceType,
			Labels:     d.Labels,
			State:      state,
		}
		profiles = append(profiles, p)
		byState[state.String()]++
		byType[d.DeviceType]++
	}

	return &simulatorv1.FleetStatus{
		TotalDevices: int32(len(profiles)),
		ByState:      byState,
		ByType:       byType,
		Devices:      profiles,
	}
}

// Shutdown stops all devices and cancels the manager context.
func (m *Manager) Shutdown() {
	m.cancel()
}

// resolveSelector returns device IDs matching the selector.
func (m *Manager) resolveSelector(selector *simulatorv1.DeviceSelector) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if selector == nil {
		ids := make([]string, 0, len(m.devices))
		for id := range m.devices {
			ids = append(ids, id)
		}
		return ids
	}

	switch s := selector.Selector.(type) {
	case *simulatorv1.DeviceSelector_DeviceIds:
		return s.DeviceIds.Ids

	case *simulatorv1.DeviceSelector_LabelSelector:
		return m.resolveByLabel(s.LabelSelector)

	default:
		return nil
	}
}

// resolveByLabel parses "key=value" label selectors and returns matching device IDs.
func (m *Manager) resolveByLabel(expr string) []string {
	parts := strings.SplitN(expr, "=", 2)
	if len(parts) != 2 {
		return nil
	}
	key, val := parts[0], parts[1]

	var ids []string
	for id, d := range m.devices {
		if d.Labels[key] == val {
			ids = append(ids, id)
		}
	}
	return ids
}

// matchesSelector returns true if device d matches the given selector.
func (m *Manager) matchesSelector(id string, d *VirtualDevice, selector *simulatorv1.DeviceSelector) bool {
	switch s := selector.Selector.(type) {
	case *simulatorv1.DeviceSelector_DeviceIds:
		for _, sid := range s.DeviceIds.Ids {
			if sid == id {
				return true
			}
		}
		return false
	case *simulatorv1.DeviceSelector_LabelSelector:
		parts := strings.SplitN(s.LabelSelector, "=", 2)
		if len(parts) != 2 {
			return false
		}
		return d.Labels[parts[0]] == parts[1]
	}
	return true
}

// buildGenerators constructs generators from the DeviceSpec's behavior_params.
func (m *Manager) buildGenerators(spec *simulatorv1.DeviceSpec) (map[string]generator.Generator, error) {
	if spec.BehaviorParams == nil {
		return map[string]generator.Generator{}, nil
	}

	cfg := generator.StructToMap(spec.BehaviorParams)
	fields, ok := cfg["fields"].(map[string]any)
	if !ok {
		return map[string]generator.Generator{}, nil
	}

	gens := make(map[string]generator.Generator, len(fields))
	for fieldName, rawCfg := range fields {
		fieldCfg, ok := rawCfg.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field %q config is not an object", fieldName)
		}
		g, err := generator.NewFromConfig(fieldCfg, fieldName, 0)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fieldName, err)
		}
		gens[fieldName] = g
	}
	return gens, nil
}

// publisherForProtocol returns the publisher for the given protocol string.
// Phase 1 only supports console; real protocols are added in Phase 3.
func (m *Manager) publisherForProtocol(proto string) protocol.Publisher {
	return protocol.NewConsolePublisher()
}

// durationFromProto converts a protobuf Duration to time.Duration.
func durationFromProto(d *durationpb.Duration) time.Duration {
	return d.AsDuration()
}
