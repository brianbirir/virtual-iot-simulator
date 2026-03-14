package device

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"github.com/virtual-iot-simulator/device-runtime/internal/generator"
	"github.com/virtual-iot-simulator/device-runtime/internal/protocol"
)

const (
	defaultTelemetryBufferSize = 10_000
	defaultEventsBufferSize    = 1_000
)

// ManagerConfig holds runtime-wide configuration for the Manager.
type ManagerConfig struct {
	// MasterSeed is the top-level RNG seed. 0 means non-deterministic.
	// deviceSeed = MasterSeed XOR fnv64a(deviceID)
	MasterSeed int64

	// RunID is a unique identifier for this runtime instance (used for log correlation
	// and deterministic replay). If empty, the runtime generates one at startup.
	RunID string

	// BackpressureStrategy is applied to all devices: "drop_oldest" (default) | "slow_down".
	BackpressureStrategy string

	// Protocol adapter configs — used by publisherForProtocol.
	MQTT protocol.MQTTConfig
	HTTP protocol.HTTPConfig
	AMQP protocol.AMQPConfig
}

// Manager owns the fleet lifecycle: spawning, stopping, and querying devices.
type Manager struct {
	devices     map[string]*VirtualDevice
	mu          sync.RWMutex
	clock       *RuntimeClock
	telemetryCh chan *simulatorv1.TelemetryPoint
	eventsCh    chan *simulatorv1.DeviceEvent

	cfg        ManagerConfig
	publishers map[string]protocol.Publisher // protocol → cached shared publisher
	pubMu      sync.RWMutex

	wg sync.WaitGroup // tracks running device goroutines for graceful shutdown

	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a Manager with the given config.
func NewManager(cfg ManagerConfig) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		devices:     make(map[string]*VirtualDevice),
		clock:       NewRuntimeClock(),
		telemetryCh: make(chan *simulatorv1.TelemetryPoint, defaultTelemetryBufferSize),
		eventsCh:    make(chan *simulatorv1.DeviceEvent, defaultEventsBufferSize),
		cfg:         cfg,
		publishers:  make(map[string]protocol.Publisher),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// TelemetryCh returns the read side of the fan-in telemetry channel.
func (m *Manager) TelemetryCh() <-chan *simulatorv1.TelemetryPoint {
	return m.telemetryCh
}

// EventsCh returns the read side of the device lifecycle events channel.
func (m *Manager) EventsCh() <-chan *simulatorv1.DeviceEvent {
	return m.eventsCh
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

	// Derive device-level seed: masterSeed XOR fnv64a(deviceID)
	deviceSeed := generator.DeriveSeed(m.cfg.MasterSeed, spec.DeviceId)
	gens, err := m.buildGenerators(spec, deviceSeed)
	if err != nil {
		return fmt.Errorf("building generators: %w", err)
	}

	proto := spec.Protocol
	if proto == "" {
		proto = "console"
	}

	d := NewVirtualDevice(DeviceConfig{
		ID:                   spec.DeviceId,
		DeviceType:           spec.DeviceType,
		Protocol:             proto,
		Labels:               spec.Labels,
		Interval:             interval,
		Publisher:            pub,
		Topic:                topic,
		Generators:           gens,
		Clock:                m.clock,
		TelemetryCh:          m.telemetryCh,
		TelemetryCap:         cap(m.telemetryCh),
		EventsCh:             m.eventsCh,
		BackpressureStrategy: m.cfg.BackpressureStrategy,
	})

	m.devices[spec.DeviceId] = d
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
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

	profiles := make([]*simulatorv1.DeviceProfile, 0, len(m.devices))
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

// InjectFault injects a fault into all devices matching selector.
// Returns injected count and failure map.
func (m *Manager) InjectFault(
	selector *simulatorv1.DeviceSelector,
	faultType simulatorv1.FaultType,
	duration time.Duration,
	params map[string]any,
) (int, map[string]string) {
	ids := m.resolveSelector(selector)
	failures := map[string]string{}
	injected := 0

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, id := range ids {
		d, ok := m.devices[id]
		if !ok {
			failures[id] = "device not found"
			continue
		}
		d.AddFault(ActiveFault{
			Type:      faultType,
			StartedAt: time.Now(),
			Duration:  duration,
			Params:    params,
		})
		injected++
	}
	return injected, failures
}

// UpdateBehavior replaces generator configs for devices matching selector.
// Returns updated count and failure map.
func (m *Manager) UpdateBehavior(
	selector *simulatorv1.DeviceSelector,
	params *structpb.Struct,
) (int, map[string]string) {
	ids := m.resolveSelector(selector)
	failures := map[string]string{}
	updated := 0

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, id := range ids {
		d, ok := m.devices[id]
		if !ok {
			failures[id] = "device not found"
			continue
		}
		deviceSeed := generator.DeriveSeed(m.cfg.MasterSeed, id)
		gens, err := m.buildGenerators(&simulatorv1.DeviceSpec{
			DeviceId:       id,
			BehaviorParams: params,
		}, deviceSeed)
		if err != nil {
			failures[id] = err.Error()
			continue
		}
		d.UpdateGenerators(gens)
		updated++
	}
	return updated, failures
}

// Shutdown stops all devices and cancels the manager context.
// It blocks until all device goroutines have exited, logging progress
// every 5 seconds so operators can observe slow-stopping devices.
func (m *Manager) Shutdown() {
	m.mu.RLock()
	total := len(m.devices)
	m.mu.RUnlock()

	log.Info().Int("devices", total).Msg("stopping all devices")
	m.cancel()

	// Poll wg.Wait() with periodic progress logs.
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			log.Info().Msg("all devices stopped")
			goto closePublishers
		case <-ticker.C:
			m.mu.RLock()
			remaining := len(m.devices)
			m.mu.RUnlock()
			log.Info().Int("remaining", remaining).Int("total", total).Msg("waiting for devices to stop…")
		}
	}

closePublishers:
	m.pubMu.Lock()
	for _, p := range m.publishers {
		p.Close() //nolint:errcheck
	}
	m.pubMu.Unlock()
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
func (m *Manager) buildGenerators(spec *simulatorv1.DeviceSpec, deviceSeed int64) (map[string]generator.Generator, error) {
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
		g, err := generator.NewFromConfig(fieldCfg, fieldName, deviceSeed)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fieldName, err)
		}
		gens[fieldName] = g
	}
	return gens, nil
}

// publisherForProtocol returns a shared publisher for the given protocol string.
// Falls back to ConsolePublisher on connection errors.
func (m *Manager) publisherForProtocol(proto string) protocol.Publisher {
	if proto == "" {
		proto = "console"
	}

	m.pubMu.RLock()
	if p, ok := m.publishers[proto]; ok {
		m.pubMu.RUnlock()
		return p
	}
	m.pubMu.RUnlock()

	m.pubMu.Lock()
	defer m.pubMu.Unlock()
	// Double-check after acquiring write lock.
	if p, ok := m.publishers[proto]; ok {
		return p
	}

	p := m.createPublisher(proto)
	m.publishers[proto] = p
	return p
}

// createPublisher constructs the protocol-specific publisher or falls back to console.
func (m *Manager) createPublisher(proto string) protocol.Publisher {
	switch proto {
	case "mqtt":
		if m.cfg.MQTT.BrokerURL == "" {
			log.Warn().Msg("MQTT broker URL not configured, falling back to console")
			return protocol.NewConsolePublisher()
		}
		poolSize := m.cfg.MQTT.PoolSize
		if poolSize < 2 {
			p, err := protocol.NewMQTTPublisher(m.cfg.MQTT)
			if err != nil {
				log.Warn().Err(err).Msg("MQTT publisher failed, falling back to console")
				return protocol.NewConsolePublisher()
			}
			return p
		}
		pool, err := protocol.NewMQTTPool(m.cfg.MQTT)
		if err != nil {
			log.Warn().Err(err).Int("pool_size", poolSize).Msg("MQTT pool failed, falling back to console")
			return protocol.NewConsolePublisher()
		}
		log.Info().Int("pool_size", poolSize).Msg("MQTT connection pool created")
		return pool

	case "http":
		if m.cfg.HTTP.Endpoint == "" {
			log.Warn().Msg("HTTP endpoint not configured, falling back to console")
			return protocol.NewConsolePublisher()
		}
		return protocol.NewHTTPPublisher(m.cfg.HTTP)

	case "amqp":
		if m.cfg.AMQP.URL == "" {
			log.Warn().Msg("AMQP URL not configured, falling back to console")
			return protocol.NewConsolePublisher()
		}
		p, err := protocol.NewAMQPPublisher(m.cfg.AMQP)
		if err != nil {
			log.Warn().Err(err).Msg("AMQP publisher failed, falling back to console")
			return protocol.NewConsolePublisher()
		}
		return p

	default:
		return protocol.NewConsolePublisher()
	}
}

// durationFromProto converts a protobuf Duration to time.Duration.
func durationFromProto(d *durationpb.Duration) time.Duration {
	return d.AsDuration()
}
