package tools_test

import (
	"context"
	"fmt"
	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/tools"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// --- Helper functions ---

// makeRequest builds a CallToolRequest with the given arguments.
func makeRequest(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// makeEmbedding creates a []any embedding of the given dimension with float64 values.
func makeEmbedding(dim int) []any {
	emb := make([]any, dim)
	for i := range emb {
		emb[i] = float64(i) * 0.01
	}
	return emb
}

// assertErrorResult checks that the result is an error result and that the text
// content contains the expected prefix.
func assertErrorResult(t *testing.T, result *mcp.CallToolResult, err error, expectedPrefix string) {
	t.Helper()
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("handler returned nil result")
	}
	if !result.IsError {
		t.Fatal("expected IsError to be true, got false")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content item in error result")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.HasPrefix(tc.Text, expectedPrefix) {
		t.Errorf("expected error text to start with %q, got %q", expectedPrefix, tc.Text)
	}
}

// =============================================================================
// store_data validation tests (no DB needed — errors before repo call)
// =============================================================================

// TestStoreData_InvalidEmbeddingDimension verifies that store_data returns a
// validation error when the embedding does not have exactly 384 dimensions.
// Validates: Requirements 4.4, 14.1, 14.2, 14.3
func TestStoreData_InvalidEmbeddingDimension(t *testing.T) {
	handler := tools.StoreHandler(nil) // nil repo — validation fails before DB call

	req := makeRequest(map[string]any{
		"key":       "test-key",
		"content":   "test-content",
		"embedding": makeEmbedding(256), // wrong dimension
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// TestStoreData_EmptyKey verifies that store_data returns a validation error
// when the key is empty.
// Validates: Requirements 4.5, 14.1
func TestStoreData_EmptyKey(t *testing.T) {
	handler := tools.StoreHandler(nil)

	req := makeRequest(map[string]any{
		"key":       "",
		"content":   "test-content",
		"embedding": makeEmbedding(384),
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// TestStoreData_EmptyContent verifies that store_data returns a validation error
// when the content is empty.
// Validates: Requirements 4.6, 14.1
func TestStoreData_EmptyContent(t *testing.T) {
	handler := tools.StoreHandler(nil)

	req := makeRequest(map[string]any{
		"key":       "test-key",
		"content":   "",
		"embedding": makeEmbedding(384),
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// TestStoreData_InvalidMetadataType verifies that store_data returns a validation
// error when metadata is not a JSON object.
// Validates: Requirements 4.7, 14.2
func TestStoreData_InvalidMetadataType(t *testing.T) {
	handler := tools.StoreHandler(nil)

	req := makeRequest(map[string]any{
		"key":       "test-key",
		"content":   "test-content",
		"metadata":  "not-an-object", // string instead of map
		"embedding": makeEmbedding(384),
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// =============================================================================
// query_similar validation tests (no DB needed)
// =============================================================================

// TestQuerySimilar_InvalidEmbeddingDimension verifies that query_similar returns
// a validation error when the embedding does not have exactly 384 dimensions.
// Validates: Requirements 5.6, 14.1, 14.2
func TestQuerySimilar_InvalidEmbeddingDimension(t *testing.T) {
	handler := tools.QueryHandler(nil)

	req := makeRequest(map[string]any{
		"embedding": makeEmbedding(128), // wrong dimension
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// =============================================================================
// get_data validation tests (no DB needed)
// =============================================================================

// TestGetData_InvalidUUID verifies that get_data returns a validation error
// when the id is not a valid UUID.
// Validates: Requirements 6.4, 14.2
func TestGetData_InvalidUUID(t *testing.T) {
	handler := tools.GetHandler(nil)

	req := makeRequest(map[string]any{
		"id": "not-a-uuid",
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// =============================================================================
// update_data validation tests (no DB needed)
// =============================================================================

// TestUpdateData_InvalidUUID verifies that update_data returns a validation error
// when the id is not a valid UUID.
// Validates: Requirements 8.7, 14.2
func TestUpdateData_InvalidUUID(t *testing.T) {
	handler := tools.UpdateHandler(nil)

	req := makeRequest(map[string]any{
		"id":  "not-a-uuid",
		"key": "new-key",
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// TestUpdateData_EmptyUpdate verifies that update_data returns a validation error
// when no optional fields are provided.
// Validates: Requirements 8.5, 14.1
func TestUpdateData_EmptyUpdate(t *testing.T) {
	handler := tools.UpdateHandler(nil)

	req := makeRequest(map[string]any{
		"id": "550e8400-e29b-41d4-a716-446655440000", // valid UUID, but no fields to update
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// TestUpdateData_InvalidEmbeddingDimension verifies that update_data returns a
// validation error when the embedding does not have exactly 384 dimensions.
// Validates: Requirements 8.6, 14.2
func TestUpdateData_InvalidEmbeddingDimension(t *testing.T) {
	handler := tools.UpdateHandler(nil)

	req := makeRequest(map[string]any{
		"id":        "550e8400-e29b-41d4-a716-446655440000",
		"embedding": makeEmbedding(100), // wrong dimension
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// TestUpdateData_InvalidMetadataType verifies that update_data returns a validation
// error when metadata is not a JSON object.
// Validates: Requirements 8.8, 14.2
func TestUpdateData_InvalidMetadataType(t *testing.T) {
	handler := tools.UpdateHandler(nil)

	req := makeRequest(map[string]any{
		"id":       "550e8400-e29b-41d4-a716-446655440000",
		"metadata": []any{"not", "an", "object"}, // array instead of map
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// =============================================================================
// delete_data validation tests (no DB needed)
// =============================================================================

// TestDeleteData_InvalidUUID verifies that delete_data returns a validation error
// when the id is not a valid UUID.
// Validates: Requirements 9.4, 14.2
func TestDeleteData_InvalidUUID(t *testing.T) {
	handler := tools.DeleteHandler(nil)

	req := makeRequest(map[string]any{
		"id": "not-a-uuid",
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "validation:")
}

// =============================================================================
// Not-found tests (require testcontainer with real DB)
// =============================================================================

// setupTestRepo creates a testcontainer with PostgreSQL+pgvector, initializes
// the schema, and returns a *db.Repository. The container is cleaned up when
// the test finishes.
func setupTestRepo(t *testing.T) *db.Repository {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
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

	if err := db.InitSchema(ctx, pool); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	return db.NewRepository(pool)
}

// nonExistentUUID is a valid UUID that does not exist in the database.
const nonExistentUUID = "00000000-0000-4000-8000-000000000000"

// TestGetData_NotFound verifies that get_data returns a not-found error when
// the UUID does not exist in the database.
// Validates: Requirements 6.3, 14.3
func TestGetData_NotFound(t *testing.T) {
	repo := setupTestRepo(t)
	handler := tools.GetHandler(repo)

	req := makeRequest(map[string]any{
		"id": nonExistentUUID,
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "not found:")
}

// TestUpdateData_NotFound verifies that update_data returns a not-found error
// when the UUID does not exist in the database.
// Validates: Requirements 8.4, 14.3
func TestUpdateData_NotFound(t *testing.T) {
	repo := setupTestRepo(t)
	handler := tools.UpdateHandler(repo)

	req := makeRequest(map[string]any{
		"id":  nonExistentUUID,
		"key": "updated-key",
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "not found:")
}

// TestDeleteData_NotFound verifies that delete_data returns a not-found error
// when the UUID does not exist in the database.
// Validates: Requirements 9.3, 14.3
func TestDeleteData_NotFound(t *testing.T) {
	repo := setupTestRepo(t)
	handler := tools.DeleteHandler(repo)

	req := makeRequest(map[string]any{
		"id": nonExistentUUID,
	})

	result, err := handler(context.Background(), req)
	assertErrorResult(t, result, err, "not found:")
}
