package protocol

import (
	"context"
	"fmt"
	"io"
	"os"
)

// ConsolePublisher writes telemetry as "[topic] <payload>" to a writer (default: stdout).
// It is the development/testing sink — no broker required.
type ConsolePublisher struct {
	w io.Writer
}

// NewConsolePublisher creates a ConsolePublisher writing to stdout.
func NewConsolePublisher() *ConsolePublisher {
	return &ConsolePublisher{w: os.Stdout}
}

// NewConsolePublisherWriter creates a ConsolePublisher writing to w (useful for tests).
func NewConsolePublisherWriter(w io.Writer) *ConsolePublisher {
	return &ConsolePublisher{w: w}
}

// Publish writes "[topic] payload\n" to the configured writer.
func (c *ConsolePublisher) Publish(_ context.Context, topic string, payload []byte) error {
	_, err := fmt.Fprintf(c.w, "[%s] %s\n", topic, payload)
	return err
}

// Close is a no-op for the console publisher.
func (c *ConsolePublisher) Close() error { return nil }
