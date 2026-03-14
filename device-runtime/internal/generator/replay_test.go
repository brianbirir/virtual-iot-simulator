package generator_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/virtual-iot-simulator/device-runtime/internal/generator"
)

// TestDeterministicReplay verifies that every generator type produces an
// identical sequence of values when seeded with the same master seed and
// device/field identifiers. This property is required so that recorded
// simulation runs can be replayed exactly for debugging or regression testing.
func TestDeterministicReplay(t *testing.T) {
	const masterSeed int64 = 42
	deviceID := "device-001"
	baseTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	steps := 20

	cases := []struct {
		name      string
		fieldName string
		cfg       map[string]any
	}{
		{
			name:      "gaussian",
			fieldName: "temperature",
			cfg:       map[string]any{"type": "gaussian", "mean": 25.0, "stddev": 2.0},
		},
		{
			name:      "static",
			fieldName: "firmware_version",
			cfg:       map[string]any{"type": "static", "value": "1.0.0"},
		},
		{
			name:      "brownian",
			fieldName: "pressure",
			cfg: map[string]any{
				"type":           "brownian",
				"initial":        1013.0,
				"mean":           1013.0,
				"mean_reversion": 0.1,
				"volatility":     0.5,
				"drift":          0.0,
				"min":            950.0,
				"max":            1080.0,
			},
		},
		{
			name:      "diurnal",
			fieldName: "solar_irradiance",
			cfg: map[string]any{
				"type":      "diurnal",
				"baseline":  400.0,
				"amplitude": 300.0,
				"peak_hour": 12,
				"noise_dev": 5.0,
			},
		},
		{
			name:      "markov",
			fieldName: "status",
			cfg: map[string]any{
				"type":   "markov",
				"states": []any{"ok", "warn", "error"},
				"transition_matrix": []any{
					[]any{0.9, 0.08, 0.02},
					[]any{0.3, 0.6, 0.1},
					[]any{0.5, 0.3, 0.2},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deviceSeed := generator.DeriveSeed(masterSeed, deviceID)

			run := func() []any {
				g, err := generator.NewFromConfig(tc.cfg, tc.fieldName, deviceSeed)
				if err != nil {
					t.Fatalf("NewFromConfig: %v", err)
				}
				vals := make([]any, steps)
				for i := 0; i < steps; i++ {
					vals[i] = g.Next(baseTime.Add(time.Duration(i)*time.Second), nil)
				}
				return vals
			}

			first := run()
			second := run()

			for i := range first {
				if fmt.Sprintf("%v", first[i]) != fmt.Sprintf("%v", second[i]) {
					t.Errorf("step %d: run1=%v run2=%v — not deterministic", i, first[i], second[i])
				}
			}
		})
	}
}
