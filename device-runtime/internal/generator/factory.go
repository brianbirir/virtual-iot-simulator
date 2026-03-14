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
// The "type" key determines the generator kind. seed is the base seed; per-field
// seeds are derived as baseSeed XOR hash(fieldName).
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
