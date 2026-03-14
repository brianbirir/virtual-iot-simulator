package generator

import (
	"math"
	"math/rand"
	"time"
)

// DiurnalGenerator produces sinusoidal values tied to the simulation clock's
// time of day, optionally overlaid with Gaussian noise.
//
// Formula:
//
//	value = baseline + amplitude × sin(2π × (hour − (peakHour − 6)) / 24)
//	      + noiseDev × N(0,1)
//
// The −6 phase shift ensures the sine wave peaks at peakHour rather than
// 6 hours before it.
type DiurnalGenerator struct {
	Baseline  float64
	Amplitude float64
	PeakHour  int
	NoiseDev  float64
	rng       *rand.Rand
}

// NewDiurnal creates a DiurnalGenerator. peakHour is 0–23 (e.g. 14 for 2 pm).
// noiseDev=0 produces a clean sinusoid.
func NewDiurnal(baseline, amplitude float64, peakHour int, noiseDev float64, seed int64) *DiurnalGenerator {
	return &DiurnalGenerator{
		Baseline:  baseline,
		Amplitude: amplitude,
		PeakHour:  peakHour,
		NoiseDev:  noiseDev,
		rng:       rand.New(rand.NewSource(seed)), //nolint:gosec
	}
}

// Next computes the sinusoidal value at the simulation clock's current time.
func (g *DiurnalGenerator) Next(now time.Time, _ map[string]any) any {
	hour := float64(now.Hour()) + float64(now.Minute())/60.0 + float64(now.Second())/3600.0
	phase := 2 * math.Pi * (hour - float64(g.PeakHour-6)) / 24.0
	value := g.Baseline + g.Amplitude*math.Sin(phase)
	if g.NoiseDev > 0 {
		value += g.NoiseDev * g.rng.NormFloat64()
	}
	return value
}
