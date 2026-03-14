package device

import (
	"time"

	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
)

// ActiveFault represents a fault currently applied to a virtual device.
type ActiveFault struct {
	Type      simulatorv1.FaultType
	StartedAt time.Time
	Duration  time.Duration // 0 means permanent until explicitly cleared
	Params    map[string]any
}

// IsExpired returns true if the fault's configured duration has elapsed.
func (f ActiveFault) IsExpired(now time.Time) bool {
	return f.Duration > 0 && now.After(f.StartedAt.Add(f.Duration))
}
