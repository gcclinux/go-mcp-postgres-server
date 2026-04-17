package db_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"go-mcp-postgres-server/db"
)

// TestInitSchemaIdempotency runs InitSchema twice on a testcontainer and verifies
// no errors occur on the second run. This confirms all DDL uses IF NOT EXISTS.
// Validates: Requirements 3.6, 10.1, 10.2
func TestInitSchemaIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Skip if Docker is not available
	testcontainers.SkipIfProviderIsNotHealthy(t)

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg17",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start testcontainer: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate testcontainer: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}
	mappedPort, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("failed to get mapped port: %v", err)
	}

	dsn := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, mappedPort.Port())

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// First run
	if err := db.InitSchema(ctx, pool); err != nil {
		t.Fatalf("InitSchema (first run) failed: %v", err)
	}

	// Second run — must succeed without errors (idempotent)
	if err := db.InitSchema(ctx, pool); err != nil {
		t.Fatalf("InitSchema (second run) failed: %v", err)
	}
}

// TestPrintSchemaOutput verifies that PrintSchema writes the SchemaDDL constant
// to the provided writer.
// Validates: Requirements 10.1, 10.3
func TestPrintSchemaOutput(t *testing.T) {
	var buf bytes.Buffer
	db.PrintSchema(&buf)

	output := buf.String()

	// Must match SchemaDDL exactly
	if output != db.SchemaDDL {
		t.Fatalf("PrintSchema output does not match SchemaDDL constant.\nGot length: %d\nWant length: %d", len(output), len(db.SchemaDDL))
	}

	// Verify key DDL statements are present
	expectedStatements := []string{
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE TABLE IF NOT EXISTS documents",
		"CREATE INDEX IF NOT EXISTS idx_documents_embedding_hnsw",
		"CREATE INDEX IF NOT EXISTS idx_documents_namespace",
		"CREATE INDEX IF NOT EXISTS idx_documents_metadata",
	}
	for _, stmt := range expectedStatements {
		if !strings.Contains(output, stmt) {
			t.Errorf("PrintSchema output missing expected statement: %s", stmt)
		}
	}
}
