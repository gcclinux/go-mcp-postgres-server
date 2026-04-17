package tools

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/models"
	"go-mcp-postgres-server/validator"
)

// NewListTool defines the list_data MCP tool with its parameter schema.
func NewListTool() mcp.Tool {
	return mcp.NewTool("list_data",
		mcp.WithDescription("List records with pagination and optional namespace and metadata filtering"),
		mcp.WithNumber("limit", mcp.Description("Maximum number of records to return (default 20)")),
		mcp.WithNumber("offset", mcp.Description("Number of records to skip for pagination (default 0)")),
		mcp.WithString("namespace", mcp.Description("Filter results to this namespace")),
		mcp.WithObject("metadata_filter", mcp.Description("Filter results to records whose metadata contains these key-value pairs")),
	)
}

// listResponse is the JSON structure returned by the list_data tool.
type listResponse struct {
	Records    []models.DocumentSummary `json:"records"`
	TotalCount int64                    `json:"total_count"`
}

// ListHandler returns a ToolHandlerFunc that lists documents via the repository.
func ListHandler(repo *db.Repository) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestID := uuid.New().String()
		slog.Info("tool invoked", "tool", "list_data", "request_id", requestID)

		args := req.GetArguments()

		// Extract limit with default of 20
		limit := 20
		if rawLimit, exists := args["limit"]; exists && rawLimit != nil {
			if l, ok := rawLimit.(float64); ok {
				limit = int(l)
			}
		}
		if err := validator.ValidatePositiveInt("limit", limit); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Extract offset with default of 0
		offset := 0
		if rawOffset, exists := args["offset"]; exists && rawOffset != nil {
			if o, ok := rawOffset.(float64); ok {
				offset = int(o)
			}
		}
		if err := validator.ValidatePositiveInt("offset", offset); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
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

		// Build list params
		params := models.ListParams{
			Limit:          limit,
			Offset:         offset,
			Namespace:      namespace,
			MetadataFilter: metadataFilter,
		}

		// Execute list query
		records, totalCount, err := repo.List(ctx, params)
		if err != nil {
			slog.Error("list_data failed", "tool", "list_data", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		// Ensure records is never null in JSON output
		if records == nil {
			records = []models.DocumentSummary{}
		}

		// Marshal response with records and total_count
		resp := listResponse{
			Records:    records,
			TotalCount: totalCount,
		}

		data, err := json.Marshal(resp)
		if err != nil {
			slog.Error("list_data marshal failed", "tool", "list_data", "request_id", requestID, "error", err)
			return mcp.NewToolResultError("internal error: please try again later"), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}
