package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/validator"
)

// NewDeleteTool defines the delete_data MCP tool with its parameter schema.
func NewDeleteTool() mcp.Tool {
	return mcp.NewTool("delete_data",
		mcp.WithDescription("Delete a document by UUID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("UUID of the document to delete")),
	)
}

// DeleteHandler returns a ToolHandlerFunc that deletes a document by ID via the repository.
func DeleteHandler(repo *db.Repository) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestID := uuid.New().String()
		slog.Info("tool invoked", "tool", "delete_data", "request_id", requestID)

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

		// Delete document from database
		deleted, err := repo.Delete(ctx, id)
		if err != nil {
			if strings.HasPrefix(err.Error(), "not found:") {
				return mcp.NewToolResultError(err.Error()), nil
			}
			slog.Error("delete_data failed", "tool", "delete_data", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		// If no row was deleted, the document does not exist
		if !deleted {
			return mcp.NewToolResultError(fmt.Sprintf("not found: document with id '%s' does not exist", id)), nil
		}

		// Return success as JSON
		return mcp.NewToolResultText(fmt.Sprintf(`{"deleted":true,"id":"%s"}`, id)), nil
	}
}
