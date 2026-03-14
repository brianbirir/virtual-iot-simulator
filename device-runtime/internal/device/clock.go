package device

import (
	"sync"
	"time"
)

// RuntimeClock provides simulation time to all devices in the runtime.
// When speed == 1.0, Now() == time.Now(). Higher speeds advance simulation time faster.
type RuntimeClock struct {
	mu        sync.RWMutex
	simStart  time.Time
	realStart time.Time
	speed     float64
}

// NewRuntimeClock creates a clock running at real-time (speed 1.0).
func NewRuntimeClock() *RuntimeClock {
	now := time.Now()
	return &RuntimeClock{
		simStart:  now,
		realStart: now,
		speed:     1.0,
	}
}

// Set updates the simulation epoch and speed multiplier.
// Devices spawned after this call will use the new time base.
func (c *RuntimeClock) Set(simEpoch time.Time, speed float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.simStart = simEpoch
	c.realStart = time.Now()
	c.speed = speed
}

// Now returns the current simulation time.
func (c *RuntimeClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	elapsed := time.Since(c.realStart)
	simElapsed := time.Duration(float64(elapsed) * c.speed)
	return c.simStart.Add(simElapsed)
}
