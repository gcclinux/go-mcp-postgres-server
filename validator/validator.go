package validator

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ValidateEmbedding checks that the embedding has exactly 384 dimensions.
func ValidateEmbedding(embedding []float64) error {
	if len(embedding) != 384 {
		return fmt.Errorf("validation: embedding must have exactly 384 dimensions, got %d", len(embedding))
	}
	return nil
}

// ValidateUUID parses and returns a UUID or a validation error.
func ValidateUUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("validation: invalid UUID %q: %w", s, err)
	}
	return id, nil
}

// ValidateNonEmpty checks that a string is non-empty after trimming whitespace.
func ValidateNonEmpty(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("validation: %s must not be empty", field)
	}
	return nil
}

// ValidateMetadata checks that metadata is a valid JSON object (map), not array/scalar/null.
func ValidateMetadata(v any) (map[string]any, error) {
	if v == nil {
		return nil, fmt.Errorf("validation: metadata must be a JSON object, got null")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("validation: metadata must be a JSON object, got %T", v)
	}
	return m, nil
}

// ValidatePositiveInt checks that an integer value is non-negative.
func ValidatePositiveInt(field string, v int) error {
	if v < 0 {
		return fmt.Errorf("validation: %s must be non-negative, got %d", field, v)
	}
	return nil
}
