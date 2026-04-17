package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/validator"
)

// NewGetTool defines the get_data MCP tool with its parameter schema.
func NewGetTool() mcp.Tool {
	return mcp.NewTool("get_data",
		mcp.WithDescription("Retrieve a document by UUID, including the full embedding"),
		mcp.WithString("id", mcp.Required(), mcp.Description("UUID of the document to retrieve")),
	)
}

// GetHandler returns a ToolHandlerFunc that retrieves a document by ID via the repository.
func GetHandler(repo *db.Repository) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestID := uuid.New().String()
		slog.Info("tool invoked", "tool", "get_data", "request_id", requestID)

		// Extract "id" parameter
		args := req.GetArguments()
		rawID, ok := args["id"].(string)
		if !ok {
			return mcp.NewToolResultError("validation: id must be a string"), nil
		}

		// Validate UUID format
		id, err := validator.ValidateUUID(rawID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Retrieve document from database
		doc, err := repo.GetByID(ctx, id)
		if err != nil {
			if strings.HasPrefix(err.Error(), "not found:") {
				return mcp.NewToolResultError(err.Error()), nil
			}
			slog.Error("get_data failed", "tool", "get_data", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		// Marshal document to JSON
		data, err := json.Marshal(doc)
		if err != nil {
			slog.Error("get_data marshal failed", "tool", "get_data", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}
