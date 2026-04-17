package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/tools"
)

// --- Shared helpers ---

// setupIntegrationDB starts a PostgreSQL+pgvector testcontainer, creates a pool,
// initialises the schema, and returns a *db.Repository. The container and pool
// are cleaned up when the test finishes.
func setupIntegrationDB(t *testing.T) *db.Repository {
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

// makeRequest builds a CallToolRequest with the given arguments.
func makeRequest(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// makeEmbedding384 creates a []any embedding of 384 dimensions with distinct values.
func makeEmbedding384(seed float64) []any {
	emb := make([]any, 384)
	for i := range emb {
		emb[i] = seed + float64(i)*0.001
	}
	return emb
}

// extractTextContent extracts the text from the first TextContent item of a
// successful tool result.
func extractTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result.IsError {
		tc, ok := result.Content[0].(mcp.TextContent)
		if ok {
			t.Fatalf("tool returned error: %s", tc.Text)
		}
		t.Fatalf("tool returned error result")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected at least one content item")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// =============================================================================
// TestFullCRUDLifecycle — end-to-end CRUD through tool handlers
// =============================================================================

// TestFullCRUDLifecycle tests the complete CRUD lifecycle by calling tool
// handlers directly against a real PostgreSQL+pgvector testcontainer:
//
//	store_data → get_data → update_data → list_data → query_similar → delete_data
//
// Validates: Requirements 1.1, 1.2, 4.2, 5.2, 6.2, 7.2, 8.2, 9.2
func TestFullCRUDLifecycle(t *testing.T) {
	repo := setupIntegrationDB(t)
	ctx := context.Background()

	// Create all handlers
	storeHandler := tools.StoreHandler(repo)
	getHandler := tools.GetHandler(repo)
	updateHandler := tools.UpdateHandler(repo)
	listHandler := tools.ListHandler(repo)
	queryHandler := tools.QueryHandler(repo)
	deleteHandler := tools.DeleteHandler(repo)

	// --- Step 1: store_data ---
	embedding := makeEmbedding384(0.1)
	storeResult, err := storeHandler(ctx, makeRequest(map[string]any{
		"key":       "integration-key",
		"content":   "integration content body",
		"namespace": "test-ns",
		"metadata":  map[string]any{"env": "test", "priority": "high"},
		"embedding": embedding,
	}))
	if err != nil {
		t.Fatalf("store_data handler error: %v", err)
	}
	docID := extractTextContent(t, storeResult)
	if len(docID) == 0 {
		t.Fatal("store_data returned empty ID")
	}
	t.Logf("stored document ID: %s", docID)

	// --- Step 2: get_data ---
	getResult, err := getHandler(ctx, makeRequest(map[string]any{
		"id": docID,
	}))
	if err != nil {
		t.Fatalf("get_data handler error: %v", err)
	}
	getJSON := extractTextContent(t, getResult)

	var getDoc map[string]any
	if err := json.Unmarshal([]byte(getJSON), &getDoc); err != nil {
		t.Fatalf("failed to unmarshal get_data response: %v", err)
	}

	// Verify fields match what we stored
	if getDoc["key"] != "integration-key" {
		t.Errorf("expected key 'integration-key', got %v", getDoc["key"])
	}
	if getDoc["content"] != "integration content body" {
		t.Errorf("expected content 'integration content body', got %v", getDoc["content"])
	}
	if getDoc["namespace"] != "test-ns" {
		t.Errorf("expected namespace 'test-ns', got %v", getDoc["namespace"])
	}
	meta, ok := getDoc["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata to be a map, got %T", getDoc["metadata"])
	}
	if meta["env"] != "test" || meta["priority"] != "high" {
		t.Errorf("metadata mismatch: got %v", meta)
	}

	// --- Step 3: update_data ---
	updateResult, err := updateHandler(ctx, makeRequest(map[string]any{
		"id":      docID,
		"key":     "updated-key",
		"content": "updated content body",
	}))
	if err != nil {
		t.Fatalf("update_data handler error: %v", err)
	}
	updateJSON := extractTextContent(t, updateResult)

	var updatedDoc map[string]any
	if err := json.Unmarshal([]byte(updateJSON), &updatedDoc); err != nil {
		t.Fatalf("failed to unmarshal update_data response: %v", err)
	}
	if updatedDoc["key"] != "updated-key" {
		t.Errorf("expected updated key 'updated-key', got %v", updatedDoc["key"])
	}
	if updatedDoc["content"] != "updated content body" {
		t.Errorf("expected updated content 'updated content body', got %v", updatedDoc["content"])
	}
	// Namespace should be unchanged
	if updatedDoc["namespace"] != "test-ns" {
		t.Errorf("expected namespace to remain 'test-ns', got %v", updatedDoc["namespace"])
	}

	// --- Step 4: list_data ---
	listResult, err := listHandler(ctx, makeRequest(map[string]any{
		"namespace": "test-ns",
	}))
	if err != nil {
		t.Fatalf("list_data handler error: %v", err)
	}
	listJSON := extractTextContent(t, listResult)

	var listResp map[string]any
	if err := json.Unmarshal([]byte(listJSON), &listResp); err != nil {
		t.Fatalf("failed to unmarshal list_data response: %v", err)
	}
	records, ok := listResp["records"].([]any)
	if !ok {
		t.Fatalf("expected records to be an array, got %T", listResp["records"])
	}
	if len(records) < 1 {
		t.Fatal("expected at least 1 record in list_data response")
	}
	// Verify the updated document appears in the list
	found := false
	for _, r := range records {
		rec, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if rec["key"] == "updated-key" {
			found = true
			break
		}
	}
	if !found {
		t.Error("updated document not found in list_data results")
	}

	// Verify total_count
	totalCount, ok := listResp["total_count"].(float64)
	if !ok {
		t.Fatalf("expected total_count to be a number, got %T", listResp["total_count"])
	}
	if totalCount < 1 {
		t.Errorf("expected total_count >= 1, got %v", totalCount)
	}

	// --- Step 5: query_similar ---
	queryResult, err := queryHandler(ctx, makeRequest(map[string]any{
		"embedding": embedding, // same embedding we stored
		"namespace": "test-ns",
		"limit":     float64(5),
	}))
	if err != nil {
		t.Fatalf("query_similar handler error: %v", err)
	}
	queryJSON := extractTextContent(t, queryResult)

	var queryResults []map[string]any
	if err := json.Unmarshal([]byte(queryJSON), &queryResults); err != nil {
		t.Fatalf("failed to unmarshal query_similar response: %v", err)
	}
	if len(queryResults) < 1 {
		t.Fatal("expected at least 1 result from query_similar")
	}
	// The document should appear with high similarity since we used the same embedding
	topResult := queryResults[0]
	if topResult["key"] != "updated-key" {
		t.Errorf("expected top result key 'updated-key', got %v", topResult["key"])
	}
	similarity, ok := topResult["similarity"].(float64)
	if !ok {
		t.Fatalf("expected similarity to be a number, got %T", topResult["similarity"])
	}
	if similarity < 0.99 {
		t.Errorf("expected high similarity (>= 0.99), got %v", similarity)
	}

	// --- Step 6: delete_data ---
	deleteResult, err := deleteHandler(ctx, makeRequest(map[string]any{
		"id": docID,
	}))
	if err != nil {
		t.Fatalf("delete_data handler error: %v", err)
	}
	deleteJSON := extractTextContent(t, deleteResult)
	if !strings.Contains(deleteJSON, `"deleted":true`) {
		t.Errorf("expected delete confirmation, got: %s", deleteJSON)
	}

	// Verify the document is gone via get_data
	getAfterDelete, err := getHandler(ctx, makeRequest(map[string]any{
		"id": docID,
	}))
	if err != nil {
		t.Fatalf("get_data after delete handler error: %v", err)
	}
	if !getAfterDelete.IsError {
		t.Error("expected get_data to return error after deletion")
	}
	tc, ok := getAfterDelete.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent in error result")
	}
	if !strings.HasPrefix(tc.Text, "not found:") {
		t.Errorf("expected 'not found:' error, got: %s", tc.Text)
	}
}

// =============================================================================
// TestConcurrentToolInvocations — concurrent store and retrieve
// =============================================================================

// TestConcurrentToolInvocations verifies that multiple goroutines can
// concurrently store and retrieve documents without errors or data corruption.
// Validates: Requirements 1.4
func TestConcurrentToolInvocations(t *testing.T) {
	repo := setupIntegrationDB(t)
	ctx := context.Background()

	storeHandler := tools.StoreHandler(repo)
	getHandler := tools.GetHandler(repo)

	const numGoroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, numGoroutines*2)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			key := fmt.Sprintf("concurrent-key-%d", idx)
			content := fmt.Sprintf("concurrent content %d", idx)
			embedding := makeEmbedding384(float64(idx) * 0.01)

			// Store
			storeResult, err := storeHandler(ctx, makeRequest(map[string]any{
				"key":       key,
				"content":   content,
				"namespace": "concurrent-ns",
				"embedding": embedding,
			}))
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: store error: %w", idx, err)
				return
			}
			if storeResult.IsError {
				tc, _ := storeResult.Content[0].(mcp.TextContent)
				errs <- fmt.Errorf("goroutine %d: store returned error: %s", idx, tc.Text)
				return
			}

			docID := ""
			if tc, ok := storeResult.Content[0].(mcp.TextContent); ok {
				docID = tc.Text
			}

			// Retrieve and verify
			getResult, err := getHandler(ctx, makeRequest(map[string]any{
				"id": docID,
			}))
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: get error: %w", idx, err)
				return
			}
			if getResult.IsError {
				tc, _ := getResult.Content[0].(mcp.TextContent)
				errs <- fmt.Errorf("goroutine %d: get returned error: %s", idx, tc.Text)
				return
			}

			tc, ok := getResult.Content[0].(mcp.TextContent)
			if !ok {
				errs <- fmt.Errorf("goroutine %d: expected TextContent", idx)
				return
			}

			var doc map[string]any
			if err := json.Unmarshal([]byte(tc.Text), &doc); err != nil {
				errs <- fmt.Errorf("goroutine %d: unmarshal error: %w", idx, err)
				return
			}

			if doc["key"] != key {
				errs <- fmt.Errorf("goroutine %d: expected key %q, got %v", idx, key, doc["key"])
				return
			}
			if doc["content"] != content {
				errs <- fmt.Errorf("goroutine %d: expected content %q, got %v", idx, content, doc["content"])
				return
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// =============================================================================
// TestSSEServerStartsAndAcceptsConnections — SSE transport smoke test
// =============================================================================

// TestSSEServerStartsAndAcceptsConnections verifies that the SSE server starts,
// listens on the configured port, and accepts HTTP connections to the SSE
// endpoint.
// Validates: Requirements 1.1, 1.2, 1.3
func TestSSEServerStartsAndAcceptsConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	// Start a testcontainer for the database
	containerReq := testcontainers.ContainerRequest{
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
		ContainerRequest: containerReq,
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

	repo := db.NewRepository(pool)

	// Create MCP server with all tools registered
	mcpServer := server.NewMCPServer("go-mcp-postgres-server", "1.0.0",
		server.WithRecovery(),
	)
	mcpServer.AddTool(tools.NewStoreTool(), tools.StoreHandler(repo))
	mcpServer.AddTool(tools.NewQueryTool(), tools.QueryHandler(repo))
	mcpServer.AddTool(tools.NewGetTool(), tools.GetHandler(repo))
	mcpServer.AddTool(tools.NewListTool(), tools.ListHandler(repo))
	mcpServer.AddTool(tools.NewUpdateTool(), tools.UpdateHandler(repo))
	mcpServer.AddTool(tools.NewDeleteTool(), tools.DeleteHandler(repo))

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	// Create and start SSE server
	sseServer := server.NewSSEServer(mcpServer,
		server.WithBaseURL(fmt.Sprintf("http://%s", addr)),
	)

	sseErrCh := make(chan error, 1)
	go func() {
		sseErrCh <- sseServer.Start(addr)
	}()

	// Wait for the server to be ready
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Make an HTTP GET request to the SSE endpoint to verify it's listening
	client := &http.Client{Timeout: 5 * time.Second}
	sseURL := fmt.Sprintf("http://%s/sse", addr)

	resp, err := client.Get(sseURL)
	if err != nil {
		t.Fatalf("failed to connect to SSE endpoint: %v", err)
	}
	defer resp.Body.Close()

	// SSE endpoint should return 200 OK with text/event-stream content type
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("expected Content-Type to contain 'text/event-stream', got %q", contentType)
	}

	// Shutdown the SSE server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sseServer.Shutdown(shutdownCtx); err != nil {
		t.Errorf("SSE server shutdown error: %v", err)
	}
}

// =============================================================================
// TestGracefulShutdown — graceful shutdown via sseServer.Shutdown()
// =============================================================================

// TestGracefulShutdown verifies that calling sseServer.Shutdown() with a 30s
// timeout context completes without error, and that the server no longer
// accepts connections after shutdown. This tests the same code path that the
// SIGINT/SIGTERM signal handler triggers in main.go.
// Validates: Requirements 13.1, 13.2, 13.3, 13.4
func TestGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	// Start a testcontainer for the database
	containerReq := testcontainers.ContainerRequest{
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
		ContainerRequest: containerReq,
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
	// Pool will be closed as part of the shutdown test, but ensure cleanup
	// in case the test fails early.
	poolClosed := false
	t.Cleanup(func() {
		if !poolClosed {
			pool.Close()
		}
	})

	if err := db.InitSchema(ctx, pool); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	repo := db.NewRepository(pool)

	// Create MCP server with all tools registered
	mcpServer := server.NewMCPServer("go-mcp-postgres-server", "1.0.0",
		server.WithRecovery(),
	)
	mcpServer.AddTool(tools.NewStoreTool(), tools.StoreHandler(repo))
	mcpServer.AddTool(tools.NewQueryTool(), tools.QueryHandler(repo))
	mcpServer.AddTool(tools.NewGetTool(), tools.GetHandler(repo))
	mcpServer.AddTool(tools.NewListTool(), tools.ListHandler(repo))
	mcpServer.AddTool(tools.NewUpdateTool(), tools.UpdateHandler(repo))
	mcpServer.AddTool(tools.NewDeleteTool(), tools.DeleteHandler(repo))

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	// Create and start SSE server
	sseServer := server.NewSSEServer(mcpServer,
		server.WithBaseURL(fmt.Sprintf("http://%s", addr)),
	)

	sseErrCh := make(chan error, 1)
	go func() {
		sseErrCh <- sseServer.Start(addr)
	}()

	// Wait for the server to be ready
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify the server is accepting connections before shutdown
	preConn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("server not accepting connections before shutdown: %v", err)
	}
	preConn.Close()

	// Initiate graceful shutdown with a 30s timeout (matching main.go behavior)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shutdownStart := time.Now()
	if err := sseServer.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("sseServer.Shutdown returned error: %v", err)
	}
	shutdownDuration := time.Since(shutdownStart)

	// Verify shutdown completed within the 30s timeout
	if shutdownDuration > 30*time.Second {
		t.Errorf("shutdown took %v, expected within 30s", shutdownDuration)
	}
	t.Logf("shutdown completed in %v", shutdownDuration)

	// Close the pool (mirroring main.go behavior after SSE shutdown)
	pool.Close()
	poolClosed = true

	// Verify the server is no longer accepting connections
	_, err = net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil {
		t.Error("expected connection to fail after shutdown, but it succeeded")
	}
}
