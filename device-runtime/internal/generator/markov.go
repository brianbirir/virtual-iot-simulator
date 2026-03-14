package generator

import (
	"math/rand"
	"time"
)

// MarkovGenerator produces discrete state transitions governed by a
// probability transition matrix. Each call to Next samples the next state
// from the row corresponding to the current state.
type MarkovGenerator struct {
	States           []string
	TransitionMatrix [][]float64
	currentIdx       int
	rng              *rand.Rand
}

// NewMarkov creates a MarkovGenerator. states is the ordered list of state
// names; matrix[i][j] is the probability of transitioning from state i to
// state j (each row must sum to 1). initialState must be a member of states.
func NewMarkov(states []string, matrix [][]float64, initialState string, seed int64) *MarkovGenerator {
	idx := 0
	for i, s := range states {
		if s == initialState {
			idx = i
			break
		}
	}
	return &MarkovGenerator{
		States:           states,
		TransitionMatrix: matrix,
		currentIdx:       idx,
		rng:              rand.New(rand.NewSource(seed)), //nolint:gosec
	}
}

// Next samples the transition matrix and returns the new state name.
func (g *MarkovGenerator) Next(_ time.Time, _ map[string]any) any {
	if len(g.States) == 0 || len(g.TransitionMatrix) == 0 {
		return ""
	}
	row := g.TransitionMatrix[g.currentIdx]
	r := g.rng.Float64()
	cumulative := 0.0
	for i, prob := range row {
		cumulative += prob
		if r < cumulative {
			g.currentIdx = i
			return g.States[i]
		}
	}
	// Floating-point rounding safety: stay in current state.
	return g.States[g.currentIdx]
}
