package generator

import (
	"fmt"
	"hash/fnv"

	"google.golang.org/protobuf/types/known/structpb"
)

// StructToMap converts a proto Struct to map[string]any, avoiding repetitive
// GetStringValue() / GetNumberValue() calls throughout the factory.
func StructToMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

// NewFromConfig creates a Generator from a config map (derived from a proto Struct).
// The "type" key determines the generator kind. baseSeed is the device-level seed;
// per-field seeds are derived as baseSeed XOR fnv64a(fieldName).
func NewFromConfig(config map[string]any, fieldName string, baseSeed int64) (Generator, error) {
	if config == nil {
		return nil, fmt.Errorf("generator config is nil for field %q", fieldName)
	}

	genType, ok := config["type"].(string)
	if !ok || genType == "" {
		return nil, fmt.Errorf("generator config for field %q missing required key \"type\"", fieldName)
	}

	seed := deriveSeed(baseSeed, fieldName)

	switch genType {
	case "gaussian":
		mean, err := requireFloat(config, "mean", fieldName)
		if err != nil {
			return nil, err
		}
		stddev, err := requireFloat(config, "stddev", fieldName)
		if err != nil {
			return nil, err
		}
		return NewGaussian(mean, stddev, seed), nil

	case "static":
		val, ok := config["value"]
		if !ok {
			return nil, fmt.Errorf("static generator for field %q missing required key \"value\"", fieldName)
		}
		return NewStatic(val), nil

	case "brownian":
		start := optFloat(config, "start")
		drift := optFloat(config, "drift")
		volatility, err := requireFloat(config, "volatility", fieldName)
		if err != nil {
			return nil, err
		}
		meanReversion := optFloat(config, "mean_reversion")
		mean := optFloat(config, "mean")
		min := optFloat(config, "min")
		max := optFloat(config, "max")
		return NewBrownian(start, drift, volatility, meanReversion, mean, min, max, seed), nil

	case "diurnal":
		baseline, err := requireFloat(config, "baseline", fieldName)
		if err != nil {
			return nil, err
		}
		amplitude, err := requireFloat(config, "amplitude", fieldName)
		if err != nil {
			return nil, err
		}
		peakHour := int(optFloat(config, "peak_hour"))
		noiseDev := optFloat(config, "noise_stddev")
		if noiseDev == 0 {
			noiseDev = optFloat(config, "stddev") // accept stddev alias
		}
		return NewDiurnal(baseline, amplitude, peakHour, noiseDev, seed), nil

	case "markov":
		states, err := requireStringSlice(config, "states", fieldName)
		if err != nil {
			return nil, err
		}
		matrix, err := requireMatrix(config, "transition_matrix", fieldName)
		if err != nil {
			return nil, err
		}
		initialState := optString(config, "initial_state")
		if initialState == "" && len(states) > 0 {
			initialState = states[0]
		}
		return NewMarkov(states, matrix, initialState, seed), nil

	default:
		return nil, fmt.Errorf("unknown generator type %q for field %q", genType, fieldName)
	}
}

// deriveSeed XORs baseSeed with an FNV hash of fieldName for per-field determinism.
func deriveSeed(base int64, fieldName string) int64 {
	h := fnv.New64a()
	h.Write([]byte(fieldName))
	return base ^ int64(h.Sum64()) //nolint:gosec
}

// DeriveSeed is the exported form used by the Manager to build device seeds.
// deviceSeed = masterSeed XOR fnv64a(deviceID)
func DeriveSeed(masterSeed int64, deviceID string) int64 {
	return deriveSeed(masterSeed, deviceID)
}

// requireFloat extracts a float64 from config[key], returning a descriptive error if missing.
func requireFloat(config map[string]any, key, fieldName string) (float64, error) {
	v, ok := config[key]
	if !ok {
		return 0, fmt.Errorf("generator config for field %q missing required key %q", fieldName, key)
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("generator config for field %q key %q must be a number, got %T", fieldName, key, v)
	}
	return f, nil
}

// optFloat returns a float64 from config[key], or 0 if absent or wrong type.
func optFloat(config map[string]any, key string) float64 {
	v, _ := config[key]
	f, _ := v.(float64)
	return f
}

// optString returns a string from config[key], or "" if absent.
func optString(config map[string]any, key string) string {
	v, _ := config[key]
	s, _ := v.(string)
	return s
}

// requireStringSlice extracts a []string from config[key].
func requireStringSlice(config map[string]any, key, fieldName string) ([]string, error) {
	v, ok := config[key]
	if !ok {
		return nil, fmt.Errorf("generator config for field %q missing required key %q", fieldName, key)
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("generator config for field %q key %q must be a list", fieldName, key)
	}
	result := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("generator config for field %q key %q element %d must be a string", fieldName, key, i)
		}
		result[i] = s
	}
	return result, nil
}

// requireMatrix extracts a [][]float64 from config[key].
func requireMatrix(config map[string]any, key, fieldName string) ([][]float64, error) {
	v, ok := config[key]
	if !ok {
		return nil, fmt.Errorf("generator config for field %q missing required key %q", fieldName, key)
	}
	rawRows, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("generator config for field %q key %q must be a 2D list", fieldName, key)
	}
	matrix := make([][]float64, len(rawRows))
	for i, rawRow := range rawRows {
		row, ok := rawRow.([]any)
		if !ok {
			return nil, fmt.Errorf("generator config for field %q key %q row %d must be a list", fieldName, key, i)
		}
		matrix[i] = make([]float64, len(row))
		for j, cell := range row {
			f, ok := cell.(float64)
			if !ok {
				return nil, fmt.Errorf("generator config for field %q key %q[%d][%d] must be a number", fieldName, key, i, j)
			}
			matrix[i][j] = f
		}
	}
	return matrix, nil
}
