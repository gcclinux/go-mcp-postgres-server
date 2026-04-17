package tools

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	pgvector "github.com/pgvector/pgvector-go"

	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/models"
	"go-mcp-postgres-server/validator"
)

// NewStoreTool defines the store_data MCP tool with its parameter schema.
func NewStoreTool() mcp.Tool {
	return mcp.NewTool("store_data",
		mcp.WithDescription("Store a record with key, content, metadata, and embedding"),
		mcp.WithString("namespace", mcp.Description("Namespace partition, defaults to 'default'")),
		mcp.WithString("key", mcp.Required(), mcp.Description("Record key")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Record content")),
		mcp.WithObject("metadata", mcp.Description("Arbitrary JSON metadata")),
		mcp.WithArray("embedding", mcp.Required(), mcp.Description("384-dimensional float array")),
	)
}

// StoreHandler returns a ToolHandlerFunc that stores a document via the repository.
func StoreHandler(repo *db.Repository) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestID := uuid.New().String()
		slog.Info("tool invoked", "tool", "store_data", "request_id", requestID)

		// Extract parameters
		args := req.GetArguments()

		namespace := "default"
		if ns, ok := args["namespace"].(string); ok && ns != "" {
			namespace = ns
		}

		key, ok := args["key"].(string)
		if !ok {
			return mcp.NewToolResultError("validation: key must be a string"), nil
		}

		content, ok := args["content"].(string)
		if !ok {
			return mcp.NewToolResultError("validation: content must be a string"), nil
		}

		// Validate key and content are non-empty
		if err := validator.ValidateNonEmpty("key", key); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := validator.ValidateNonEmpty("content", content); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validate metadata if provided
		var metadata map[string]any
		if rawMeta, exists := args["metadata"]; exists && rawMeta != nil {
			m, err := validator.ValidateMetadata(rawMeta)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			metadata = m
		}

		// Extract and validate embedding
		rawEmbedding, ok := args["embedding"].([]any)
		if !ok {
			return mcp.NewToolResultError("validation: embedding must be an array of floats"), nil
		}

		embedding := make([]float64, len(rawEmbedding))
		for i, v := range rawEmbedding {
			f, ok := v.(float64)
			if !ok {
				return mcp.NewToolResultError("validation: embedding must contain only numeric values"), nil
			}
			embedding[i] = f
		}

		if err := validator.ValidateEmbedding(embedding); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Build document input
		doc := models.DocumentInput{
			Namespace: namespace,
			Key:       key,
			Content:   content,
			Metadata:  metadata,
			Embedding: pgvector.NewVector(float64ToFloat32(embedding)),
		}

		// Insert into database
		id, err := repo.Insert(ctx, doc)
		if err != nil {
			slog.Error("store_data failed", "tool", "store_data", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		return mcp.NewToolResultText(id.String()), nil
	}
}

// float64ToFloat32 converts a slice of float64 to float32 for pgvector.
func float64ToFloat32(f64 []float64) []float32 {
	f32 := make([]float32, len(f64))
	for i, v := range f64 {
		f32[i] = float32(v)
	}
	return f32
}
