package generator

import (
	"math/rand"
	"time"
)

// GaussianGenerator produces values drawn from a normal distribution.
type GaussianGenerator struct {
	Mean   float64
	StdDev float64
	rng    *rand.Rand
}

// NewGaussian creates a GaussianGenerator with a fixed seed for deterministic replay.
func NewGaussian(mean, stddev float64, seed int64) *GaussianGenerator {
	return &GaussianGenerator{
		Mean:   mean,
		StdDev: stddev,
		rng:    rand.New(rand.NewSource(seed)), //nolint:gosec // deterministic sim RNG
	}
}

// Next returns mean + stddev * N(0,1).
func (g *GaussianGenerator) Next(_ time.Time, _ map[string]any) any {
	return g.Mean + g.StdDev*g.rng.NormFloat64()
}

// StaticGenerator always returns a fixed value. Useful for testing and constant fields.
type StaticGenerator struct {
	Value any
}

// NewStatic creates a StaticGenerator returning value on every call.
func NewStatic(value any) *StaticGenerator {
	return &StaticGenerator{Value: value}
}

// Next returns the fixed value regardless of time or state.
func (s *StaticGenerator) Next(_ time.Time, _ map[string]any) any {
	return s.Value
}
