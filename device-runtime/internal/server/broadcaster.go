// Package server implements the gRPC server and supporting components.
package server

import (
	"sync"

	simulatorv1 "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1"
)

const subscriberBufferSize = 1000

// Broadcaster fans out TelemetryPoints from the Manager's single channel to
// multiple concurrent StreamTelemetry RPC subscribers. Each subscriber gets its
// own buffered channel; slow subscribers drop points rather than stalling others.
type Broadcaster struct {
	source      <-chan *simulatorv1.TelemetryPoint
	subscribers map[chan *simulatorv1.TelemetryPoint]struct{}
	mu          sync.Mutex
	done        chan struct{}
}

// NewBroadcaster creates a Broadcaster reading from source and starts the dispatch goroutine.
func NewBroadcaster(source <-chan *simulatorv1.TelemetryPoint) *Broadcaster {
	b := &Broadcaster{
		source:      source,
		subscribers: make(map[chan *simulatorv1.TelemetryPoint]struct{}),
		done:        make(chan struct{}),
	}
	go b.dispatch()
	return b
}

// Subscribe returns a channel that will receive TelemetryPoints. Call Unsubscribe when done.
func (b *Broadcaster) Subscribe() chan *simulatorv1.TelemetryPoint {
	ch := make(chan *simulatorv1.TelemetryPoint, subscriberBufferSize)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (b *Broadcaster) Unsubscribe(ch chan *simulatorv1.TelemetryPoint) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// Stop shuts down the dispatcher.
func (b *Broadcaster) Stop() {
	close(b.done)
}

// dispatch reads from source and fan-outs to all subscribers.
func (b *Broadcaster) dispatch() {
	for {
		select {
		case <-b.done:
			return
		case pt, ok := <-b.source:
			if !ok {
				return
			}
			b.mu.Lock()
			for ch := range b.subscribers {
				select {
				case ch <- pt:
				default: // slow subscriber — drop point
				}
			}
			b.mu.Unlock()
		}
	}
}
