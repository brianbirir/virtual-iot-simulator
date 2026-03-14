package generator

import (
	"math"
	"testing"
	"time"
)

func TestGaussianWithinBounds(t *testing.T) {
	g := NewGaussian(50.0, 10.0, 42)
	now := time.Now()
	state := map[string]any{}

	const n = 10_000
	for i := 0; i < n; i++ {
		v := g.Next(now, state).(float64)
		// Within 5σ: probability of failure ~0.00003% per sample
		if v < 50.0-5*10.0 || v > 50.0+5*10.0 {
			t.Errorf("value %f outside 5σ bounds", v)
		}
	}
}

func TestGaussianDeterministic(t *testing.T) {
	g1 := NewGaussian(0, 1, 99)
	g2 := NewGaussian(0, 1, 99)
	now := time.Now()
	state := map[string]any{}

	for i := 0; i < 100; i++ {
		v1 := g1.Next(now, state).(float64)
		v2 := g2.Next(now, state).(float64)
		if v1 != v2 {
			t.Fatalf("non-deterministic at step %d: %f != %f", i, v1, v2)
		}
	}
}

func TestGaussianMeanApproaches(t *testing.T) {
	g := NewGaussian(100.0, 5.0, 7)
	now := time.Now()
	state := map[string]any{}

	sum := 0.0
	const n = 10_000
	for i := 0; i < n; i++ {
		sum += g.Next(now, state).(float64)
	}
	mean := sum / n
	if math.Abs(mean-100.0) > 0.5 {
		t.Errorf("sample mean %f too far from 100.0", mean)
	}
}

func TestStaticGeneratorAlwaysReturnsValue(t *testing.T) {
	s := NewStatic(42.0)
	now := time.Now()
	state := map[string]any{}

	for i := 0; i < 100; i++ {
		v := s.Next(now, state)
		if v != 42.0 {
			t.Fatalf("expected 42.0, got %v", v)
		}
	}
}

func TestStaticGeneratorStringValue(t *testing.T) {
	s := NewStatic("locked")
	v := s.Next(time.Now(), map[string]any{})
	if v != "locked" {
		t.Fatalf("expected 'locked', got %v", v)
	}
}
