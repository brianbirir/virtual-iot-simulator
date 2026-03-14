// Package generator provides data generators for virtual device telemetry fields.
package generator

import "time"

// Generator produces the next value for a telemetry field.
// Implementations must be safe for single-goroutine use (no concurrent calls).
type Generator interface {
	// Next produces the next value. now is the simulation clock time.
	// state is the device's mutable state map for cross-field dependencies.
	Next(now time.Time, state map[string]any) any
}
