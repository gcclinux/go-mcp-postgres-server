package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	pgvector "github.com/pgvector/pgvector-go"

	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/models"
	"go-mcp-postgres-server/validator"
)

// NewUpdateTool defines the update_data MCP tool with its parameter schema.
func NewUpdateTool() mcp.Tool {
	return mcp.NewTool("update_data",
		mcp.WithDescription("Update an existing record by UUID, modifying only the provided fields"),
		mcp.WithString("id", mcp.Required(), mcp.Description("UUID of the document to update")),
		mcp.WithString("key", mcp.Description("New record key")),
		mcp.WithString("content", mcp.Description("New record content")),
		mcp.WithObject("metadata", mcp.Description("New arbitrary JSON metadata")),
		mcp.WithArray("embedding", mcp.Description("New 384-dimensional float array")),
		mcp.WithString("namespace", mcp.Description("New namespace partition")),
	)
}

// UpdateHandler returns a ToolHandlerFunc that updates a document via the repository.
func UpdateHandler(repo *db.Repository) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestID := uuid.New().String()
		slog.Info("tool invoked", "tool", "update_data", "request_id", requestID)

		args := req.GetArguments()

		// Extract and validate UUID from "id"
		rawID, ok := args["id"].(string)
		if !ok {
			return mcp.NewToolResultError("validation: id must be a string"), nil
		}

		id, err := validator.ValidateUUID(rawID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Build DocumentPatch from optional fields
		var patch models.DocumentPatch
		hasField := false

		if rawKey, exists := args["key"]; exists && rawKey != nil {
			k, ok := rawKey.(string)
			if !ok {
				return mcp.NewToolResultError("validation: key must be a string"), nil
			}
			patch.Key = &k
			hasField = true
		}

		if rawContent, exists := args["content"]; exists && rawContent != nil {
			c, ok := rawContent.(string)
			if !ok {
				return mcp.NewToolResultError("validation: content must be a string"), nil
			}
			patch.Content = &c
			hasField = true
		}

		if rawMeta, exists := args["metadata"]; exists && rawMeta != nil {
			m, err := validator.ValidateMetadata(rawMeta)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			patch.Metadata = &m
			hasField = true
		}

		if rawEmbedding, exists := args["embedding"]; exists && rawEmbedding != nil {
			arr, ok := rawEmbedding.([]any)
			if !ok {
				return mcp.NewToolResultError("validation: embedding must be an array of floats"), nil
			}

			embedding := make([]float64, len(arr))
			for i, v := range arr {
				f, ok := v.(float64)
				if !ok {
					return mcp.NewToolResultError("validation: embedding must contain only numeric values"), nil
				}
				embedding[i] = f
			}

			if err := validator.ValidateEmbedding(embedding); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			vec := pgvector.NewVector(float64ToFloat32(embedding))
			patch.Embedding = &vec
			hasField = true
		}

		if rawNS, exists := args["namespace"]; exists && rawNS != nil {
			ns, ok := rawNS.(string)
			if !ok {
				return mcp.NewToolResultError("validation: namespace must be a string"), nil
			}
			patch.Namespace = &ns
			hasField = true
		}

		// Ensure at least one optional field is provided
		if !hasField {
			return mcp.NewToolResultError("validation: at least one field (key, content, metadata, embedding, namespace) must be provided"), nil
		}

		// Call repo.Update
		doc, err := repo.Update(ctx, id, patch)
		if err != nil {
			if strings.HasPrefix(err.Error(), "not found:") {
				return mcp.NewToolResultError(err.Error()), nil
			}
			slog.Error("update_data failed", "tool", "update_data", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		// Marshal DocumentSummary to JSON
		data, err := json.Marshal(doc)
		if err != nil {
			slog.Error("update_data marshal failed", "tool", "update_data", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}
