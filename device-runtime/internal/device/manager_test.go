package device

import (
	"fmt"
	"testing"
	"time"

	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func makeSpec(id, deviceType string, labels map[string]string) *simulatorv1.DeviceSpec {
	return &simulatorv1.DeviceSpec{
		DeviceId:          id,
		DeviceType:        deviceType,
		Labels:            labels,
		TelemetryInterval: durationpb.New(20 * time.Millisecond),
		Protocol:          "console",
	}
}

func TestManagerSpawn100Devices(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Shutdown()

	specs := make([]*simulatorv1.DeviceSpec, 100)
	for i := range specs {
		specs[i] = makeSpec(
			fmt.Sprintf("sensor-%04d", i),
			"temperature_sensor",
			map[string]string{"batch": "1"},
		)
	}

	spawned, failures := m.Spawn(specs)
	if spawned != 100 {
		t.Fatalf("expected 100 spawned, got %d", spawned)
	}
	if len(failures) != 0 {
		t.Fatalf("unexpected failures: %v", failures)
	}

	// Allow devices to start
	time.Sleep(50 * time.Millisecond)

	status := m.GetStatus(nil)
	if status.TotalDevices != 100 {
		t.Fatalf("expected 100 total, got %d", status.TotalDevices)
	}
}

func TestManagerStopByLabelSelector(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Shutdown()

	// Spawn 10 "alpha" and 5 "beta"
	alphaSpecs := make([]*simulatorv1.DeviceSpec, 10)
	for i := range alphaSpecs {
		alphaSpecs[i] = makeSpec(fmt.Sprintf("alpha-%d", i), "sensor", map[string]string{"group": "alpha"})
	}
	betaSpecs := make([]*simulatorv1.DeviceSpec, 5)
	for i := range betaSpecs {
		betaSpecs[i] = makeSpec(fmt.Sprintf("beta-%d", i), "sensor", map[string]string{"group": "beta"})
	}

	m.Spawn(alphaSpecs)
	m.Spawn(betaSpecs)

	stopped, _ := m.Stop(&simulatorv1.DeviceSelector{
		Selector: &simulatorv1.DeviceSelector_LabelSelector{LabelSelector: "group=alpha"},
	}, true)

	if stopped != 10 {
		t.Fatalf("expected 10 stopped, got %d", stopped)
	}

	time.Sleep(50 * time.Millisecond)
	status := m.GetStatus(nil)
	if status.TotalDevices != 5 {
		t.Fatalf("expected 5 remaining devices, got %d", status.TotalDevices)
	}
}

func TestManagerDuplicateIDReturnsFailure(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Shutdown()

	spec := makeSpec("dup-device", "sensor", nil)
	spawned, failures := m.Spawn([]*simulatorv1.DeviceSpec{spec, spec})

	if spawned != 1 {
		t.Fatalf("expected 1 spawned, got %d", spawned)
	}
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(failures))
	}
	if _, ok := failures["dup-device"]; !ok {
		t.Fatal("expected failure for dup-device")
	}
}
