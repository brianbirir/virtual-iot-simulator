package protocol

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConsolePublisherFormat(t *testing.T) {
	var buf bytes.Buffer
	p := NewConsolePublisherWriter(&buf)

	payload := []byte(`{"device_id":"d1","temperature":22.5}`)
	if err := p.Publish(context.Background(), "devices/d1/telemetry", payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "[devices/d1/telemetry]") {
		t.Errorf("output missing topic prefix: %q", got)
	}
	if !strings.Contains(got, `"temperature":22.5`) {
		t.Errorf("output missing payload: %q", got)
	}
}

func TestConsolePublisherClose(t *testing.T) {
	p := NewConsolePublisher()
	if err := p.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}
