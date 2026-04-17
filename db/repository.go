package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"

	"go-mcp-postgres-server/models"
)

// Repository provides data access methods for the documents table.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new Repository backed by the given connection pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Insert stores a new document and returns its generated UUID.
func (r *Repository) Insert(ctx context.Context, doc models.DocumentInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx,
		`INSERT INTO documents (namespace, key, content, metadata, embedding)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		doc.Namespace, doc.Key, doc.Content, doc.Metadata, pgvector.NewVector(doc.Embedding.Slice()),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("db: insert document: %w", err)
	}
	return id, nil
}

// QuerySimilar performs cosine similarity search with optional namespace and
// metadata filtering. Results are ordered by decreasing similarity.
func (r *Repository) QuerySimilar(ctx context.Context, params models.QueryParams) ([]models.DocumentResult, error) {
	var nsFilter *string
	if params.Namespace != nil {
		nsFilter = params.Namespace
	}

	var metaBytes []byte
	if params.MetadataFilter != nil {
		b, err := json.Marshal(params.MetadataFilter)
		if err != nil {
			return nil, fmt.Errorf("db: marshal metadata filter: %w", err)
		}
		metaBytes = b
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, namespace, key, content, metadata,
		        1 - (embedding <=> $1) AS similarity,
		        created_at, updated_at
		 FROM documents
		 WHERE ($2::text IS NULL OR namespace = $2)
		   AND ($3::jsonb IS NULL OR metadata @> $3)
		 ORDER BY embedding <=> $1
		 LIMIT $4`,
		pgvector.NewVector(params.Embedding.Slice()), nsFilter, metaBytes, params.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("db: query similar: %w", err)
	}
	defer rows.Close()

	var results []models.DocumentResult
	for rows.Next() {
		var dr models.DocumentResult
		if err := rows.Scan(
			&dr.ID, &dr.Namespace, &dr.Key, &dr.Content, &dr.Metadata,
			&dr.Similarity, &dr.CreatedAt, &dr.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("db: scan similar result: %w", err)
		}
		results = append(results, dr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: query similar rows: %w", err)
	}
	return results, nil
}

// GetByID retrieves a single document by UUID, including the embedding.
// Returns a "not found:" prefixed error if no matching row exists.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*models.Document, error) {
	var doc models.Document
	err := r.pool.QueryRow(ctx,
		`SELECT id, namespace, key, content, metadata, embedding, created_at, updated_at
		 FROM documents
		 WHERE id = $1`,
		id,
	).Scan(&doc.ID, &doc.Namespace, &doc.Key, &doc.Content, &doc.Metadata,
		&doc.Embedding, &doc.CreatedAt, &doc.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("not found: document with id '%s' does not exist", id)
		}
		return nil, fmt.Errorf("db: get document by id: %w", err)
	}
	return &doc, nil
}

// List returns paginated documents with optional namespace and metadata
// filtering. Returns the matching records (without embeddings) and the total
// count of matching documents. Records are ordered by created_at DESC.
func (r *Repository) List(ctx context.Context, params models.ListParams) ([]models.DocumentSummary, int64, error) {
	var nsFilter *string
	if params.Namespace != nil {
		nsFilter = params.Namespace
	}

	var metaBytes []byte
	if params.MetadataFilter != nil {
		b, err := json.Marshal(params.MetadataFilter)
		if err != nil {
			return nil, 0, fmt.Errorf("db: marshal metadata filter: %w", err)
		}
		metaBytes = b
	}

	// Count query
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*)
		 FROM documents
		 WHERE ($1::text IS NULL OR namespace = $1)
		   AND ($2::jsonb IS NULL OR metadata @> $2)`,
		nsFilter, metaBytes,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("db: list count: %w", err)
	}

	// Data query
	rows, err := r.pool.Query(ctx,
		`SELECT id, namespace, key, content, metadata, created_at, updated_at
		 FROM documents
		 WHERE ($1::text IS NULL OR namespace = $1)
		   AND ($2::jsonb IS NULL OR metadata @> $2)
		 ORDER BY created_at DESC
		 LIMIT $3 OFFSET $4`,
		nsFilter, metaBytes, params.Limit, params.Offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: list query: %w", err)
	}
	defer rows.Close()

	var records []models.DocumentSummary
	for rows.Next() {
		var ds models.DocumentSummary
		if err := rows.Scan(
			&ds.ID, &ds.Namespace, &ds.Key, &ds.Content, &ds.Metadata,
			&ds.CreatedAt, &ds.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("db: scan list result: %w", err)
		}
		records = append(records, ds)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: list rows: %w", err)
	}
	return records, total, nil
}

// Update patches the specified fields of a document using the COALESCE
// pattern. Only non-nil fields in the patch are applied. Sets updated_at to
// now(). Returns the updated document summary.
// Returns a "not found:" prefixed error if no matching row exists.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, patch models.DocumentPatch) (*models.DocumentSummary, error) {
	var metaBytes []byte
	if patch.Metadata != nil {
		b, err := json.Marshal(*patch.Metadata)
		if err != nil {
			return nil, fmt.Errorf("db: marshal metadata patch: %w", err)
		}
		metaBytes = b
	}

	var embeddingParam *pgvector.Vector
	if patch.Embedding != nil {
		v := pgvector.NewVector(patch.Embedding.Slice())
		embeddingParam = &v
	}

	var doc models.DocumentSummary
	err := r.pool.QueryRow(ctx,
		`UPDATE documents
		 SET key = COALESCE($2, key),
		     content = COALESCE($3, content),
		     metadata = COALESCE($4, metadata),
		     embedding = COALESCE($5, embedding),
		     namespace = COALESCE($6, namespace),
		     updated_at = now()
		 WHERE id = $1
		 RETURNING id, namespace, key, content, metadata, created_at, updated_at`,
		id, patch.Key, patch.Content, metaBytes, embeddingParam, patch.Namespace,
	).Scan(&doc.ID, &doc.Namespace, &doc.Key, &doc.Content, &doc.Metadata,
		&doc.CreatedAt, &doc.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("not found: document with id '%s' does not exist", id)
		}
		return nil, fmt.Errorf("db: update document: %w", err)
	}
	return &doc, nil
}

// Delete removes a document by UUID. Returns true if a row was deleted, false
// if no matching row existed.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	result, err := r.pool.Exec(ctx,
		`DELETE FROM documents WHERE id = $1`,
		id,
	)
	if err != nil {
		return false, fmt.Errorf("db: delete document: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
