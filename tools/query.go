package tools

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	pgvector "github.com/pgvector/pgvector-go"

	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/models"
	"go-mcp-postgres-server/validator"
)

// NewQueryTool defines the query_similar MCP tool with its parameter schema.
func NewQueryTool() mcp.Tool {
	return mcp.NewTool("query_similar",
		mcp.WithDescription("Find records similar to a given embedding vector, with optional namespace and metadata filtering"),
		mcp.WithArray("embedding", mcp.Required(), mcp.Description("384-dimensional float array for similarity search")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of results to return (default 10)")),
		mcp.WithString("namespace", mcp.Description("Filter results to this namespace")),
		mcp.WithObject("metadata_filter", mcp.Description("Filter results to records whose metadata contains these key-value pairs")),
	)
}

// QueryHandler returns a ToolHandlerFunc that performs similarity search via the repository.
func QueryHandler(repo *db.Repository) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestID := uuid.New().String()
		slog.Info("tool invoked", "tool", "query_similar", "request_id", requestID)

		args := req.GetArguments()

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

		// Extract limit with default of 10
		limit := 10
		if rawLimit, exists := args["limit"]; exists && rawLimit != nil {
			if l, ok := rawLimit.(float64); ok {
				limit = int(l)
			}
		}

		// Extract optional namespace
		var namespace *string
		if ns, ok := args["namespace"].(string); ok && ns != "" {
			namespace = &ns
		}

		// Extract optional metadata_filter
		var metadataFilter map[string]any
		if rawMeta, exists := args["metadata_filter"]; exists && rawMeta != nil {
			m, err := validator.ValidateMetadata(rawMeta)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			metadataFilter = m
		}

		// Build query params
		params := models.QueryParams{
			Embedding:      pgvector.NewVector(float64ToFloat32(embedding)),
			Limit:          limit,
			Namespace:      namespace,
			MetadataFilter: metadataFilter,
		}

		// Execute similarity search
		results, err := repo.QuerySimilar(ctx, params)
		if err != nil {
			slog.Error("query_similar failed", "tool", "query_similar", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		// Marshal results to JSON
		data, err := json.Marshal(results)
		if err != nil {
			slog.Error("query_similar marshal failed", "tool", "query_similar", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}
