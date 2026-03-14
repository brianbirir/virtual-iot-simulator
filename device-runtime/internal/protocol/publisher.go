// Package protocol provides telemetry publisher implementations.
package protocol

import "context"

// Publisher abstracts the telemetry delivery mechanism.
// Implementations must be safe for concurrent use across goroutines.
type Publisher interface {
	// Publish sends payload to the given topic. Returns an error if delivery fails.
	Publish(ctx context.Context, topic string, payload []byte) error
	// Close releases any underlying connections or resources.
	Close() error
}
