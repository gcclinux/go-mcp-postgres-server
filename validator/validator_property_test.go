package validator_test

import (
	"strings"
	"testing"

	"go-mcp-postgres-server/validator"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// Feature: go-mcp-postgres-server, Property 2: Embedding Dimension Validation
// **Validates: Requirements 4.4, 5.6, 8.6**
func TestProperty_EmbeddingDimensionValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random length (0 to 1000, biased to include 384)
		length := rapid.IntRange(0, 1000).Draw(t, "length")
		embedding := make([]float64, length)
		for i := range embedding {
			embedding[i] = rapid.Float64().Draw(t, "value")
		}

		err := validator.ValidateEmbedding(embedding)
		if length == 384 {
			if err != nil {
				t.Fatalf("expected no error for 384-dim embedding, got: %v", err)
			}
		} else {
			if err == nil {
				t.Fatalf("expected error for %d-dim embedding, got nil", length)
			}
		}
	})
}

// Feature: go-mcp-postgres-server, Property 3: Non-Empty Field Validation
// **Validates: Requirements 4.5, 4.6**
func TestProperty_NonEmptyFieldValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random strings, including whitespace-only strings
		isWhitespaceOnly := rapid.Bool().Draw(t, "whitespace_only")
		var value string
		if isWhitespaceOnly {
			// Generate whitespace-only string (spaces, tabs, newlines)
			wsChars := []rune{' ', '\t', '\n', '\r'}
			length := rapid.IntRange(0, 20).Draw(t, "ws_length")
			runes := make([]rune, length)
			for i := range runes {
				runes[i] = wsChars[rapid.IntRange(0, len(wsChars)-1).Draw(t, "ws_char")]
			}
			value = string(runes)
		} else {
			// Generate string with at least one non-whitespace character
			value = rapid.StringMatching(`\S.*`).Draw(t, "non_empty_value")
		}

		err := validator.ValidateNonEmpty("test_field", value)
		hasNonWhitespace := strings.TrimSpace(value) != ""

		if hasNonWhitespace && err != nil {
			t.Fatalf("expected no error for non-empty string %q, got: %v", value, err)
		}
		if !hasNonWhitespace && err == nil {
			t.Fatalf("expected error for whitespace-only string %q, got nil", value)
		}
	})
}

// Feature: go-mcp-postgres-server, Property 4: Metadata Type Validation
// **Validates: Requirements 4.7, 8.8**
func TestProperty_MetadataTypeValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Choose a random type category
		typeChoice := rapid.IntRange(0, 5).Draw(t, "type_choice")
		var input any

		switch typeChoice {
		case 0: // map[string]any (valid)
			m := make(map[string]any)
			nKeys := rapid.IntRange(0, 5).Draw(t, "num_keys")
			for i := 0; i < nKeys; i++ {
				key := rapid.String().Draw(t, "map_key")
				m[key] = rapid.String().Draw(t, "map_value")
			}
			input = m
		case 1: // string (invalid)
			input = rapid.String().Draw(t, "string_val")
		case 2: // number (invalid)
			input = rapid.Float64().Draw(t, "number_val")
		case 3: // boolean (invalid)
			input = rapid.Bool().Draw(t, "bool_val")
		case 4: // slice (invalid)
			input = []any{rapid.String().Draw(t, "slice_elem")}
		case 5: // nil (invalid)
			input = nil
		}

		result, err := validator.ValidateMetadata(input)

		if typeChoice == 0 {
			// map[string]any should succeed
			if err != nil {
				t.Fatalf("expected no error for map input, got: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result for map input")
			}
		} else {
			// all other types should fail
			if err == nil {
				t.Fatalf("expected error for type %T, got nil", input)
			}
		}
	})
}

// Feature: go-mcp-postgres-server, Property 5: UUID Format Validation
// **Validates: Requirements 6.4, 8.7, 9.4**
func TestProperty_UUIDFormatValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		isValid := rapid.Bool().Draw(t, "is_valid_uuid")
		var input string

		if isValid {
			// Generate a valid UUID v4
			input = uuid.New().String()
		} else {
			// Generate a random string that is unlikely to be a valid UUID
			input = rapid.String().Draw(t, "random_string")
		}

		result, err := validator.ValidateUUID(input)

		// Check against actual UUID parsing to determine expected behavior
		expectedUUID, parseErr := uuid.Parse(input)

		if parseErr == nil {
			// Input is a valid UUID
			if err != nil {
				t.Fatalf("expected no error for valid UUID %q, got: %v", input, err)
			}
			if result != expectedUUID {
				t.Fatalf("expected UUID %v, got %v", expectedUUID, result)
			}
		} else {
			// Input is not a valid UUID
			if err == nil {
				t.Fatalf("expected error for invalid UUID %q, got nil", input)
			}
		}
	})
}
