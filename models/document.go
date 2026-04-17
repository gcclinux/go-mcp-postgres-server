package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// DocumentInput represents the input for storing a new document.
type DocumentInput struct {
	Namespace string          `json:"namespace"`
	Key       string          `json:"key"`
	Content   string          `json:"content"`
	Metadata  map[string]any  `json:"metadata"`
	Embedding pgvector.Vector `json:"embedding"`
}

// Document represents a full document record including embedding.
type Document struct {
	ID        uuid.UUID       `json:"id"`
	Namespace string          `json:"namespace"`
	Key       string          `json:"key"`
	Content   string          `json:"content"`
	Metadata  map[string]any  `json:"metadata"`
	Embedding pgvector.Vector `json:"embedding"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// DocumentSummary represents a document without the embedding field.
type DocumentSummary struct {
	ID        uuid.UUID      `json:"id"`
	Namespace string         `json:"namespace"`
	Key       string         `json:"key"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// DocumentResult represents a document returned from a similarity search.
type DocumentResult struct {
	DocumentSummary
	Similarity float64 `json:"similarity"`
}

// QueryParams holds parameters for a similarity search query.
type QueryParams struct {
	Embedding      pgvector.Vector `json:"embedding"`
	Limit          int             `json:"limit"`
	Namespace      *string         `json:"namespace"`
	MetadataFilter map[string]any  `json:"metadata_filter"`
}

// ListParams holds parameters for listing documents with pagination.
type ListParams struct {
	Limit          int            `json:"limit"`
	Offset         int            `json:"offset"`
	Namespace      *string        `json:"namespace"`
	MetadataFilter map[string]any `json:"metadata_filter"`
}

// DocumentPatch holds optional fields for a partial document update.
type DocumentPatch struct {
	Key       *string          `json:"key"`
	Content   *string          `json:"content"`
	Metadata  *map[string]any  `json:"metadata"`
	Embedding *pgvector.Vector `json:"embedding"`
	Namespace *string          `json:"namespace"`
}
