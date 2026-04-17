package db

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SchemaDDL contains the complete schema initialization SQL for the documents
// table and its indexes. Every statement uses IF NOT EXISTS for idempotent
// execution.
const SchemaDDL = `-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Documents table
CREATE TABLE IF NOT EXISTS documents (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace  TEXT        NOT NULL DEFAULT 'default',
    key        TEXT        NOT NULL,
    content    TEXT        NOT NULL,
    metadata   JSONB       NOT NULL DEFAULT '{}',
    embedding  vector(384),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- HNSW index for cosine similarity search
CREATE INDEX IF NOT EXISTS idx_documents_embedding_hnsw
    ON documents USING hnsw (embedding vector_cosine_ops);

-- B-tree index for namespace filtering
CREATE INDEX IF NOT EXISTS idx_documents_namespace
    ON documents (namespace);

-- GIN index for JSONB metadata containment queries
CREATE INDEX IF NOT EXISTS idx_documents_metadata
    ON documents USING gin (metadata);
`

// InitSchema executes the schema DDL against the provided connection pool.
// All statements are idempotent (IF NOT EXISTS) so running this multiple times
// against the same database is safe. Returns a descriptive error if any
// statement fails.
func InitSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, SchemaDDL)
	if err != nil {
		return fmt.Errorf("db: schema initialization failed: %w", err)
	}
	return nil
}

// PrintSchema writes the complete schema DDL to the given writer. This is used
// by the --init-schema CLI flag to export the DDL without connecting to the
// database.
func PrintSchema(w io.Writer) {
	fmt.Fprint(w, SchemaDDL)
}
