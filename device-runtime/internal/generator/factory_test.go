package generator

import (
	"testing"
	"time"
)

func TestFactoryGaussianRoundTrip(t *testing.T) {
	cfg := map[string]any{"type": "gaussian", "mean": 20.0, "stddev": 2.0}
	g, err := NewFromConfig(cfg, "temperature", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v := g.Next(time.Now(), nil)
	if _, ok := v.(float64); !ok {
		t.Fatalf("expected float64, got %T", v)
	}
}

func TestFactoryStaticRoundTrip(t *testing.T) {
	cfg := map[string]any{"type": "static", "value": "locked"}
	g, err := NewFromConfig(cfg, "lock_state", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Next(time.Now(), nil) != "locked" {
		t.Fatal("static generator returned wrong value")
	}
}

func TestFactoryUnknownTypeReturnsError(t *testing.T) {
	cfg := map[string]any{"type": "magic"}
	_, err := NewFromConfig(cfg, "field", 1)
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestFactoryMissingTypeReturnsError(t *testing.T) {
	_, err := NewFromConfig(map[string]any{"mean": 5.0}, "field", 1)
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestFactoryNilConfigReturnsError(t *testing.T) {
	_, err := NewFromConfig(nil, "field", 1)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}
