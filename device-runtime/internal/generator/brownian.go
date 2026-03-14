package generator

import (
	"math"
	"math/rand"
	"time"
)

// BrownianGenerator produces values via a mean-reverting random walk
// (Ornstein-Uhlenbeck process). The formula per tick of dt seconds is:
//
//	next = current
//	     + drift×dt
//	     + volatility×√dt×N(0,1)
//	     + meanReversion×(mean−current)×dt
//	clamped to [min, max] when max > min.
type BrownianGenerator struct {
	Drift         float64
	Volatility    float64
	MeanReversion float64
	Mean          float64
	Min           float64
	Max           float64

	current  float64
	lastTime time.Time
	rng      *rand.Rand
}

// NewBrownian creates a BrownianGenerator. start is the initial value;
// seed enables deterministic replay.
func NewBrownian(start, drift, volatility, meanReversion, mean, min, max float64, seed int64) *BrownianGenerator {
	return &BrownianGenerator{
		Drift:         drift,
		Volatility:    volatility,
		MeanReversion: meanReversion,
		Mean:          mean,
		Min:           min,
		Max:           max,
		current:       start,
		rng:           rand.New(rand.NewSource(seed)), //nolint:gosec
	}
}

// Next advances the random walk by dt seconds (derived from the simulation clock).
func (g *BrownianGenerator) Next(now time.Time, _ map[string]any) any {
	dt := 1.0
	if !g.lastTime.IsZero() {
		dt = now.Sub(g.lastTime).Seconds()
		if dt <= 0 {
			dt = 0.001 // guard against zero or negative dt
		}
	}
	g.lastTime = now

	sqrtDt := math.Sqrt(dt)
	g.current = g.current +
		g.Drift*dt +
		g.Volatility*sqrtDt*g.rng.NormFloat64() +
		g.MeanReversion*(g.Mean-g.current)*dt

	if g.Max > g.Min {
		if g.current < g.Min {
			g.current = g.Min
		}
		if g.current > g.Max {
			g.current = g.Max
		}
	}

	return g.current
}
