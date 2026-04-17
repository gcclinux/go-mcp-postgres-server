package db_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"pgregory.net/rapid"

	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/models"
)

// setupTestDB creates a PostgreSQL+pgvector testcontainer, initializes the
// schema, and returns a connection pool. The container and pool are cleaned up
// automatically via t.Cleanup.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

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

	t.Cleanup(func() {
		pool.Close()
	})

	if err := db.InitSchema(ctx, pool); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	return pool
}

// Feature: go-mcp-postgres-server, Property 1: Store-Get Round Trip
// **Validates: Requirements 4.2, 4.3, 6.2**
func TestProperty_StoreGetRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	repo := db.NewRepository(pool)

	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()

		// Generate random valid document fields
		key := rapid.StringMatching(`[a-zA-Z0-9]{1,50}`).Draw(t, "key")
		content := rapid.StringMatching(`[a-zA-Z0-9 ]{1,200}`).Draw(t, "content")
		namespace := rapid.StringMatching(`[a-zA-Z0-9]{1,30}`).Draw(t, "namespace")

		// Generate random metadata with string values
		numMeta := rapid.IntRange(0, 5).Draw(t, "numMeta")
		metadata := make(map[string]any, numMeta)
		for i := 0; i < numMeta; i++ {
			mk := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, fmt.Sprintf("metaKey%d", i))
			mv := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(t, fmt.Sprintf("metaVal%d", i))
			metadata[mk] = mv
		}

		// Generate random 384-dim embedding
		embSlice := make([]float32, 384)
		for i := range embSlice {
			embSlice[i] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("emb%d", i))
		}
		embedding := pgvector.NewVector(embSlice)

		doc := models.DocumentInput{
			Namespace: namespace,
			Key:       key,
			Content:   content,
			Metadata:  metadata,
			Embedding: embedding,
		}

		// Store the document
		id, err := repo.Insert(ctx, doc)
		if err != nil {
			t.Fatalf("Insert failed: %v", err)
		}

		// Retrieve by ID
		got, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}

		// Verify ID matches
		if got.ID != id {
			t.Fatalf("ID mismatch: got %s, want %s", got.ID, id)
		}

		// Verify key matches
		if got.Key != key {
			t.Fatalf("Key mismatch: got %q, want %q", got.Key, key)
		}

		// Verify content matches
		if got.Content != content {
			t.Fatalf("Content mismatch: got %q, want %q", got.Content, content)
		}

		// Verify namespace matches
		if got.Namespace != namespace {
			t.Fatalf("Namespace mismatch: got %q, want %q", got.Namespace, namespace)
		}

		// Verify metadata matches
		if len(got.Metadata) != len(metadata) {
			t.Fatalf("Metadata length mismatch: got %d, want %d", len(got.Metadata), len(metadata))
		}
		for k, v := range metadata {
			gotV, ok := got.Metadata[k]
			if !ok {
				t.Fatalf("Metadata key %q missing from result", k)
			}
			// JSONB round-trips values as their JSON types; string values stay strings
			if fmt.Sprintf("%v", gotV) != fmt.Sprintf("%v", v) {
				t.Fatalf("Metadata[%q] mismatch: got %v, want %v", k, gotV, v)
			}
		}

		// Verify embedding matches (compare float32 slices with tolerance for
		// PostgreSQL floating-point round-trip)
		gotEmb := got.Embedding.Slice()
		if len(gotEmb) != 384 {
			t.Fatalf("Embedding dimension mismatch: got %d, want 384", len(gotEmb))
		}
		for i := 0; i < 384; i++ {
			if math.Abs(float64(gotEmb[i]-embSlice[i])) > 1e-5 {
				t.Fatalf("Embedding[%d] mismatch: got %v, want %v", i, gotEmb[i], embSlice[i])
			}
		}
	})
}

// Feature: go-mcp-postgres-server, Property 6: Similarity Search Ordering
// **Validates: Requirements 5.2**
func TestProperty_SimilaritySearchOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	repo := db.NewRepository(pool)

	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()

		// Use a unique namespace per iteration to isolate test data
		namespace := fmt.Sprintf("simorder-%s", rapid.StringMatching(`[a-z]{10}`).Draw(t, "ns"))

		// Generate between 5 and 10 documents
		numDocs := rapid.IntRange(5, 10).Draw(t, "numDocs")

		for i := 0; i < numDocs; i++ {
			embSlice := make([]float32, 384)
			for j := range embSlice {
				embSlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("doc%d_emb%d", i, j))
			}

			doc := models.DocumentInput{
				Namespace: namespace,
				Key:       fmt.Sprintf("key-%d", i),
				Content:   fmt.Sprintf("content-%d", i),
				Metadata:  map[string]any{},
				Embedding: pgvector.NewVector(embSlice),
			}

			_, err := repo.Insert(ctx, doc)
			if err != nil {
				t.Fatalf("Insert doc %d failed: %v", i, err)
			}
		}

		// Generate a random query embedding
		querySlice := make([]float32, 384)
		for j := range querySlice {
			querySlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("query_emb%d", j))
		}

		ns := namespace
		params := models.QueryParams{
			Embedding: pgvector.NewVector(querySlice),
			Limit:     100,
			Namespace: &ns,
		}

		results, err := repo.QuerySimilar(ctx, params)
		if err != nil {
			t.Fatalf("QuerySimilar failed: %v", err)
		}

		// Verify we got results
		if len(results) == 0 {
			t.Fatal("expected at least one result from QuerySimilar")
		}

		// Verify results are ordered by decreasing similarity
		for i := 0; i < len(results)-1; i++ {
			if results[i].Similarity < results[i+1].Similarity {
				t.Fatalf("results not ordered by decreasing similarity: r[%d].Similarity=%f < r[%d].Similarity=%f",
					i, results[i].Similarity, i+1, results[i+1].Similarity)
			}
		}
	})
}

// Feature: go-mcp-postgres-server, Property 7: Namespace Filtering Invariant
// **Validates: Requirements 5.3, 7.3**
func TestProperty_NamespaceFilteringInvariant(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	repo := db.NewRepository(pool)

	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()

		// Unique base prefix per iteration to isolate data
		basePrefix := fmt.Sprintf("nsfilter-%s", rapid.StringMatching(`[a-z]{10}`).Draw(t, "basePrefix"))

		// Generate 2-3 namespaces
		numNamespaces := rapid.IntRange(2, 3).Draw(t, "numNamespaces")
		namespaces := make([]string, numNamespaces)
		for i := range namespaces {
			namespaces[i] = fmt.Sprintf("%s-ns%d", basePrefix, i)
		}

		// Store 3-5 documents per namespace
		for _, ns := range namespaces {
			numDocs := rapid.IntRange(3, 5).Draw(t, fmt.Sprintf("numDocs_%s", ns))
			for d := 0; d < numDocs; d++ {
				embSlice := make([]float32, 384)
				for j := range embSlice {
					embSlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("%s_doc%d_emb%d", ns, d, j))
				}

				doc := models.DocumentInput{
					Namespace: ns,
					Key:       fmt.Sprintf("key-%d", d),
					Content:   fmt.Sprintf("content-%d", d),
					Metadata:  map[string]any{},
					Embedding: pgvector.NewVector(embSlice),
				}

				_, err := repo.Insert(ctx, doc)
				if err != nil {
					t.Fatalf("Insert failed for ns=%s doc=%d: %v", ns, d, err)
				}
			}
		}

		// Pick one namespace as the filter target
		filterIdx := rapid.IntRange(0, numNamespaces-1).Draw(t, "filterIdx")
		filterNS := namespaces[filterIdx]

		// Test QuerySimilar with namespace filter
		querySlice := make([]float32, 384)
		for j := range querySlice {
			querySlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("query_emb%d", j))
		}

		queryResults, err := repo.QuerySimilar(ctx, models.QueryParams{
			Embedding: pgvector.NewVector(querySlice),
			Limit:     100,
			Namespace: &filterNS,
		})
		if err != nil {
			t.Fatalf("QuerySimilar failed: %v", err)
		}

		// Verify every QuerySimilar result has the filtered namespace
		for i, r := range queryResults {
			if r.Namespace != filterNS {
				t.Fatalf("QuerySimilar result[%d] has namespace %q, want %q", i, r.Namespace, filterNS)
			}
		}

		// Verify we got at least some results (we stored 3-5 docs in this namespace)
		if len(queryResults) == 0 {
			t.Fatal("QuerySimilar with namespace filter returned 0 results, expected at least 1")
		}

		// Test List with namespace filter
		listResults, totalCount, err := repo.List(ctx, models.ListParams{
			Limit:     100,
			Offset:    0,
			Namespace: &filterNS,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		// Verify every List result has the filtered namespace
		for i, r := range listResults {
			if r.Namespace != filterNS {
				t.Fatalf("List result[%d] has namespace %q, want %q", i, r.Namespace, filterNS)
			}
		}

		// Verify we got at least some results
		if len(listResults) == 0 {
			t.Fatal("List with namespace filter returned 0 results, expected at least 1")
		}

		// Verify total count matches the number of returned results (since limit is high enough)
		if totalCount != int64(len(listResults)) {
			t.Fatalf("List total_count=%d does not match returned count=%d", totalCount, len(listResults))
		}
	})
}

// Feature: go-mcp-postgres-server, Property 8: Metadata Filtering Invariant
// **Validates: Requirements 5.4, 7.4**
func TestProperty_MetadataFilteringInvariant(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	repo := db.NewRepository(pool)

	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()

		// Use a unique namespace per iteration for isolation
		namespace := fmt.Sprintf("metafilter-%s", rapid.StringMatching(`[a-z]{10}`).Draw(t, "ns"))

		// Store documents with metadata {"type": "note"}
		numNotes := rapid.IntRange(2, 4).Draw(t, "numNotes")
		for i := 0; i < numNotes; i++ {
			embSlice := make([]float32, 384)
			for j := range embSlice {
				embSlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("note%d_emb%d", i, j))
			}
			doc := models.DocumentInput{
				Namespace: namespace,
				Key:       fmt.Sprintf("note-%d", i),
				Content:   fmt.Sprintf("note content %d", i),
				Metadata:  map[string]any{"type": "note"},
				Embedding: pgvector.NewVector(embSlice),
			}
			_, err := repo.Insert(ctx, doc)
			if err != nil {
				t.Fatalf("Insert note %d failed: %v", i, err)
			}
		}

		// Store documents with metadata {"type": "task"}
		numTasks := rapid.IntRange(2, 4).Draw(t, "numTasks")
		for i := 0; i < numTasks; i++ {
			embSlice := make([]float32, 384)
			for j := range embSlice {
				embSlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("task%d_emb%d", i, j))
			}
			doc := models.DocumentInput{
				Namespace: namespace,
				Key:       fmt.Sprintf("task-%d", i),
				Content:   fmt.Sprintf("task content %d", i),
				Metadata:  map[string]any{"type": "task"},
				Embedding: pgvector.NewVector(embSlice),
			}
			_, err := repo.Insert(ctx, doc)
			if err != nil {
				t.Fatalf("Insert task %d failed: %v", i, err)
			}
		}

		// Store documents with metadata {"type": "note", "priority": "high"}
		numHigh := rapid.IntRange(1, 3).Draw(t, "numHigh")
		for i := 0; i < numHigh; i++ {
			embSlice := make([]float32, 384)
			for j := range embSlice {
				embSlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("high%d_emb%d", i, j))
			}
			doc := models.DocumentInput{
				Namespace: namespace,
				Key:       fmt.Sprintf("note-high-%d", i),
				Content:   fmt.Sprintf("high priority note %d", i),
				Metadata:  map[string]any{"type": "note", "priority": "high"},
				Embedding: pgvector.NewVector(embSlice),
			}
			_, err := repo.Insert(ctx, doc)
			if err != nil {
				t.Fatalf("Insert high-priority note %d failed: %v", i, err)
			}
		}

		// Store documents with metadata {"type": "note", "priority": "low"}
		numLow := rapid.IntRange(1, 3).Draw(t, "numLow")
		for i := 0; i < numLow; i++ {
			embSlice := make([]float32, 384)
			for j := range embSlice {
				embSlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("low%d_emb%d", i, j))
			}
			doc := models.DocumentInput{
				Namespace: namespace,
				Key:       fmt.Sprintf("note-low-%d", i),
				Content:   fmt.Sprintf("low priority note %d", i),
				Metadata:  map[string]any{"type": "note", "priority": "low"},
				Embedding: pgvector.NewVector(embSlice),
			}
			_, err := repo.Insert(ctx, doc)
			if err != nil {
				t.Fatalf("Insert low-priority note %d failed: %v", i, err)
			}
		}

		// Filter with metadata_filter = {"type": "note"}
		metadataFilter := map[string]any{"type": "note"}

		// Test QuerySimilar with metadata filter
		querySlice := make([]float32, 384)
		for j := range querySlice {
			querySlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("query_emb%d", j))
		}

		ns := namespace
		queryResults, err := repo.QuerySimilar(ctx, models.QueryParams{
			Embedding:      pgvector.NewVector(querySlice),
			Limit:          100,
			Namespace:      &ns,
			MetadataFilter: metadataFilter,
		})
		if err != nil {
			t.Fatalf("QuerySimilar failed: %v", err)
		}

		// Expected count: notes + high-priority notes + low-priority notes (all have type=note)
		expectedCount := numNotes + numHigh + numLow

		// Verify we got results
		if len(queryResults) == 0 {
			t.Fatal("QuerySimilar with metadata filter returned 0 results, expected at least 1")
		}

		// Verify every QuerySimilar result's metadata contains the filter key-value pairs
		for i, r := range queryResults {
			for fk, fv := range metadataFilter {
				rv, ok := r.Metadata[fk]
				if !ok {
					t.Fatalf("QuerySimilar result[%d] metadata missing filter key %q", i, fk)
				}
				if fmt.Sprintf("%v", rv) != fmt.Sprintf("%v", fv) {
					t.Fatalf("QuerySimilar result[%d] metadata[%q]=%v, want %v", i, fk, rv, fv)
				}
			}
		}

		// Verify result count matches expected (all docs with type=note in this namespace)
		if len(queryResults) != expectedCount {
			t.Fatalf("QuerySimilar returned %d results, expected %d", len(queryResults), expectedCount)
		}

		// Test List with metadata filter
		listResults, totalCount, err := repo.List(ctx, models.ListParams{
			Limit:          100,
			Offset:         0,
			Namespace:      &ns,
			MetadataFilter: metadataFilter,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		// Verify every List result's metadata contains the filter key-value pairs
		for i, r := range listResults {
			for fk, fv := range metadataFilter {
				rv, ok := r.Metadata[fk]
				if !ok {
					t.Fatalf("List result[%d] metadata missing filter key %q", i, fk)
				}
				if fmt.Sprintf("%v", rv) != fmt.Sprintf("%v", fv) {
					t.Fatalf("List result[%d] metadata[%q]=%v, want %v", i, fk, rv, fv)
				}
			}
		}

		// Verify we got results
		if len(listResults) == 0 {
			t.Fatal("List with metadata filter returned 0 results, expected at least 1")
		}

		// Verify total count matches the number of returned results
		if totalCount != int64(len(listResults)) {
			t.Fatalf("List total_count=%d does not match returned count=%d", totalCount, len(listResults))
		}

		// Verify result count matches expected
		if int(totalCount) != expectedCount {
			t.Fatalf("List total_count=%d, expected %d", totalCount, expectedCount)
		}
	})
}

// Feature: go-mcp-postgres-server, Property 9: List Pagination Correctness
// **Validates: Requirements 7.2, 7.6**
func TestProperty_ListPaginationCorrectness(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	repo := db.NewRepository(pool)

	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()

		// Use a unique namespace per iteration for isolation
		namespace := fmt.Sprintf("listpage-%s", rapid.StringMatching(`[a-z]{10}`).Draw(t, "ns"))

		// Generate N documents (5-15)
		numDocs := rapid.IntRange(5, 15).Draw(t, "numDocs")

		for i := range numDocs {
			embSlice := make([]float32, 384)
			for j := range embSlice {
				embSlice[j] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("doc%d_emb%d", i, j))
			}

			doc := models.DocumentInput{
				Namespace: namespace,
				Key:       fmt.Sprintf("key-%d", i),
				Content:   fmt.Sprintf("content-%d", i),
				Metadata:  map[string]any{},
				Embedding: pgvector.NewVector(embSlice),
			}

			_, err := repo.Insert(ctx, doc)
			if err != nil {
				t.Fatalf("Insert doc %d failed: %v", i, err)
			}
		}

		// Generate random limit (1-N) and offset (0-N)
		limit := rapid.IntRange(1, numDocs).Draw(t, "limit")
		offset := rapid.IntRange(0, numDocs).Draw(t, "offset")

		ns := namespace
		results, totalCount, err := repo.List(ctx, models.ListParams{
			Limit:     limit,
			Offset:    offset,
			Namespace: &ns,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		// Verify 1: len(results) <= limit
		if len(results) > limit {
			t.Fatalf("result count %d exceeds limit %d", len(results), limit)
		}

		// Verify 2: total_count == N (total docs in namespace)
		if totalCount != int64(numDocs) {
			t.Fatalf("total_count=%d, expected %d", totalCount, numDocs)
		}

		// Verify 3: Results are ordered by created_at DESC
		for i := 0; i < len(results)-1; i++ {
			if results[i].CreatedAt.Before(results[i+1].CreatedAt) {
				t.Fatalf("results not ordered by created_at DESC: r[%d].CreatedAt=%v < r[%d].CreatedAt=%v",
					i, results[i].CreatedAt, i+1, results[i+1].CreatedAt)
			}
		}

		// Also verify the expected number of results based on offset
		expectedCount := numDocs - offset
		if expectedCount < 0 {
			expectedCount = 0
		}
		if expectedCount > limit {
			expectedCount = limit
		}
		if len(results) != expectedCount {
			t.Fatalf("expected %d results (numDocs=%d, offset=%d, limit=%d), got %d",
				expectedCount, numDocs, offset, limit, len(results))
		}
	})
}

// Feature: go-mcp-postgres-server, Property 10: Partial Update Preserves Unpatched Fields
// **Validates: Requirements 8.2, 8.3**
func TestProperty_PartialUpdatePreservesUnpatchedFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	repo := db.NewRepository(pool)

	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()

		// Generate a random valid document to store
		origKey := rapid.StringMatching(`[a-zA-Z0-9]{1,50}`).Draw(t, "origKey")
		origContent := rapid.StringMatching(`[a-zA-Z0-9 ]{1,200}`).Draw(t, "origContent")
		origNamespace := rapid.StringMatching(`[a-zA-Z0-9]{1,30}`).Draw(t, "origNamespace")

		numMeta := rapid.IntRange(0, 5).Draw(t, "numMeta")
		origMetadata := make(map[string]any, numMeta)
		for i := range numMeta {
			mk := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, fmt.Sprintf("origMetaKey%d", i))
			mv := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(t, fmt.Sprintf("origMetaVal%d", i))
			origMetadata[mk] = mv
		}

		embSlice := make([]float32, 384)
		for i := range embSlice {
			embSlice[i] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("origEmb%d", i))
		}

		doc := models.DocumentInput{
			Namespace: origNamespace,
			Key:       origKey,
			Content:   origContent,
			Metadata:  origMetadata,
			Embedding: pgvector.NewVector(embSlice),
		}

		// Store the document
		id, err := repo.Insert(ctx, doc)
		if err != nil {
			t.Fatalf("Insert failed: %v", err)
		}

		// Retrieve the original document to capture its timestamps
		origDoc, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID (original) failed: %v", err)
		}

		// Define the 4 patchable fields (skip embedding per task instructions)
		type patchField int
		const (
			fieldKey patchField = iota
			fieldContent
			fieldMetadata
			fieldNamespace
		)
		allFields := []patchField{fieldKey, fieldContent, fieldMetadata, fieldNamespace}

		// Randomly select a non-empty subset of fields to patch
		// Use a bitmask approach: draw a random int in [1, 15] (at least one bit set)
		bitmask := rapid.IntRange(1, 15).Draw(t, "fieldBitmask")

		var selectedFields []patchField
		for i, f := range allFields {
			if bitmask&(1<<i) != 0 {
				selectedFields = append(selectedFields, f)
			}
		}

		// Build the patch with new random values for selected fields
		patch := models.DocumentPatch{}
		newKey := rapid.StringMatching(`[a-zA-Z0-9]{1,50}`).Draw(t, "newKey")
		newContent := rapid.StringMatching(`[a-zA-Z0-9 ]{1,200}`).Draw(t, "newContent")
		newNamespace := rapid.StringMatching(`[a-zA-Z0-9]{1,30}`).Draw(t, "newNamespace")

		newNumMeta := rapid.IntRange(0, 5).Draw(t, "newNumMeta")
		newMetadata := make(map[string]any, newNumMeta)
		for i := range newNumMeta {
			mk := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, fmt.Sprintf("newMetaKey%d", i))
			mv := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(t, fmt.Sprintf("newMetaVal%d", i))
			newMetadata[mk] = mv
		}

		selectedSet := make(map[patchField]bool)
		for _, f := range selectedFields {
			selectedSet[f] = true
			switch f {
			case fieldKey:
				patch.Key = &newKey
			case fieldContent:
				patch.Content = &newContent
			case fieldMetadata:
				patch.Metadata = &newMetadata
			case fieldNamespace:
				patch.Namespace = &newNamespace
			}
		}

		// Apply the partial update
		_, err = repo.Update(ctx, id, patch)
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		// Retrieve the updated document
		updatedDoc, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID (updated) failed: %v", err)
		}

		// Verify patched fields have new values
		if selectedSet[fieldKey] {
			if updatedDoc.Key != newKey {
				t.Fatalf("patched Key: got %q, want %q", updatedDoc.Key, newKey)
			}
		}
		if selectedSet[fieldContent] {
			if updatedDoc.Content != newContent {
				t.Fatalf("patched Content: got %q, want %q", updatedDoc.Content, newContent)
			}
		}
		if selectedSet[fieldNamespace] {
			if updatedDoc.Namespace != newNamespace {
				t.Fatalf("patched Namespace: got %q, want %q", updatedDoc.Namespace, newNamespace)
			}
		}
		if selectedSet[fieldMetadata] {
			if len(updatedDoc.Metadata) != len(newMetadata) {
				t.Fatalf("patched Metadata length: got %d, want %d", len(updatedDoc.Metadata), len(newMetadata))
			}
			for k, v := range newMetadata {
				gotV, ok := updatedDoc.Metadata[k]
				if !ok {
					t.Fatalf("patched Metadata missing key %q", k)
				}
				if fmt.Sprintf("%v", gotV) != fmt.Sprintf("%v", v) {
					t.Fatalf("patched Metadata[%q]: got %v, want %v", k, gotV, v)
				}
			}
		}

		// Verify unpatched fields retain original values
		if !selectedSet[fieldKey] {
			if updatedDoc.Key != origDoc.Key {
				t.Fatalf("unpatched Key changed: got %q, want %q", updatedDoc.Key, origDoc.Key)
			}
		}
		if !selectedSet[fieldContent] {
			if updatedDoc.Content != origDoc.Content {
				t.Fatalf("unpatched Content changed: got %q, want %q", updatedDoc.Content, origDoc.Content)
			}
		}
		if !selectedSet[fieldNamespace] {
			if updatedDoc.Namespace != origDoc.Namespace {
				t.Fatalf("unpatched Namespace changed: got %q, want %q", updatedDoc.Namespace, origDoc.Namespace)
			}
		}
		if !selectedSet[fieldMetadata] {
			if len(updatedDoc.Metadata) != len(origDoc.Metadata) {
				t.Fatalf("unpatched Metadata length changed: got %d, want %d", len(updatedDoc.Metadata), len(origDoc.Metadata))
			}
			for k, v := range origDoc.Metadata {
				gotV, ok := updatedDoc.Metadata[k]
				if !ok {
					t.Fatalf("unpatched Metadata missing key %q", k)
				}
				if fmt.Sprintf("%v", gotV) != fmt.Sprintf("%v", v) {
					t.Fatalf("unpatched Metadata[%q] changed: got %v, want %v", k, gotV, v)
				}
			}
		}

		// Verify updated_at >= original updated_at
		if updatedDoc.UpdatedAt.Before(origDoc.UpdatedAt) {
			t.Fatalf("updated_at went backwards: got %v, original %v", updatedDoc.UpdatedAt, origDoc.UpdatedAt)
		}
	})
}

// Feature: go-mcp-postgres-server, Property 11: Delete Removes Record
// **Validates: Requirements 9.2**
func TestProperty_DeleteRemovesRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	repo := db.NewRepository(pool)

	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()

		// Use a unique namespace per iteration for isolation
		namespace := fmt.Sprintf("delrec-%s", rapid.StringMatching(`[a-z]{10}`).Draw(t, "ns"))

		// Generate random valid document fields
		key := rapid.StringMatching(`[a-zA-Z0-9]{1,50}`).Draw(t, "key")
		content := rapid.StringMatching(`[a-zA-Z0-9 ]{1,200}`).Draw(t, "content")

		numMeta := rapid.IntRange(0, 5).Draw(t, "numMeta")
		metadata := make(map[string]any, numMeta)
		for i := 0; i < numMeta; i++ {
			mk := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, fmt.Sprintf("metaKey%d", i))
			mv := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(t, fmt.Sprintf("metaVal%d", i))
			metadata[mk] = mv
		}

		embSlice := make([]float32, 384)
		for i := range embSlice {
			embSlice[i] = rapid.Float32Range(-1.0, 1.0).Draw(t, fmt.Sprintf("emb%d", i))
		}

		doc := models.DocumentInput{
			Namespace: namespace,
			Key:       key,
			Content:   content,
			Metadata:  metadata,
			Embedding: pgvector.NewVector(embSlice),
		}

		// Store the document
		id, err := repo.Insert(ctx, doc)
		if err != nil {
			t.Fatalf("Insert failed: %v", err)
		}

		// Delete the document by ID
		deleted, err := repo.Delete(ctx, id)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		if !deleted {
			t.Fatal("Delete returned false, expected true for existing document")
		}

		// Verify GetByID returns a "not found:" error
		_, err = repo.GetByID(ctx, id)
		if err == nil {
			t.Fatal("GetByID after delete returned nil error, expected not-found error")
		}
		if !strings.HasPrefix(err.Error(), "not found:") {
			t.Fatalf("GetByID error does not have 'not found:' prefix: %v", err)
		}

		// Verify the document does not appear in List results (filter by namespace)
		ns := namespace
		listResults, _, err := repo.List(ctx, models.ListParams{
			Limit:     100,
			Offset:    0,
			Namespace: &ns,
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		for _, r := range listResults {
			if r.ID == id {
				t.Fatalf("deleted document %s still appears in List results", id)
			}
		}

		// Verify the document does not appear in QuerySimilar results (filter by namespace)
		queryResults, err := repo.QuerySimilar(ctx, models.QueryParams{
			Embedding: pgvector.NewVector(embSlice),
			Limit:     100,
			Namespace: &ns,
		})
		if err != nil {
			t.Fatalf("QuerySimilar failed: %v", err)
		}
		for _, r := range queryResults {
			if r.ID == id {
				t.Fatalf("deleted document %s still appears in QuerySimilar results", id)
			}
		}
	})
}
