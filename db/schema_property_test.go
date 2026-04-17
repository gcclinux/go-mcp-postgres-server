package db_test

import (
	"bytes"
	"testing"

	"go-mcp-postgres-server/db"

	"pgregory.net/rapid"
)

// Feature: go-mcp-postgres-server, Property 12: Schema DDL Consistency
// **Validates: Requirements 10.3**
func TestProperty_SchemaDDLConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Capture PrintSchema output
		var buf bytes.Buffer
		db.PrintSchema(&buf)

		// Compare byte-for-byte with SchemaDDL constant
		if buf.String() != db.SchemaDDL {
			t.Fatalf("PrintSchema output does not match SchemaDDL constant")
		}

		// Verify determinism: call again and compare
		var buf2 bytes.Buffer
		db.PrintSchema(&buf2)

		if buf.String() != buf2.String() {
			t.Fatal("PrintSchema is not deterministic across invocations")
		}
	})
}
