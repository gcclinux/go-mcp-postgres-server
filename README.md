# go-mcp-postgres-server

A Model Context Protocol (MCP) server written in Go that provides document storage and vector similarity search backed by PostgreSQL with [pgvector](https://github.com/pgvector/pgvector). It exposes six tools over SSE transport for storing, querying, listing, updating, and deleting documents with 384-dimensional embeddings.

## Features

- **Vector similarity search** using pgvector HNSW cosine index
- **Namespace partitioning** to isolate groups of documents
- **JSONB metadata filtering** on arbitrary key-value pairs
- **Pagination** with limit/offset on list operations
- **Partial updates** — modify only the fields you need
- **Auto-schema initialization** — creates tables and indexes on startup (idempotent)
- **Structured JSON logging** via `log/slog`
- **Graceful shutdown** on SIGINT/SIGTERM

## Tools

| Tool | Description |
|---|---|
| `store_data` | Store a record with key, content, metadata, and a 384-dim embedding |
| `query_similar` | Find records similar to a given embedding vector, with optional namespace and metadata filtering |
| `get_data` | Retrieve a document by UUID (includes full embedding) |
| `list_data` | List records with pagination and optional namespace/metadata filtering |
| `update_data` | Partially update an existing record by UUID |
| `delete_data` | Delete a document by UUID |

## Prerequisites

- **Go 1.25+**
- **PostgreSQL** with the [pgvector](https://github.com/pgvector/pgvector) extension installed
- **Docker** (optional, used by integration tests via testcontainers)

## Configuration

Configuration is loaded with the following priority (highest to lowest):

1. **Environment variables** already set in the process (e.g. from Docker, systemd, shell)
2. **`.env` file** in the working directory (only fills in vars not already set)
3. **Hard-coded defaults**

| Variable | Default | Description |
|---|---|---|
| `MCP_DB_HOST` | `192.168.0.65` | PostgreSQL host |
| `MCP_DB_PORT` | `5432` | PostgreSQL port |
| `MCP_DB_USER` | `$USER` | Database user |
| `MCP_DB_PASSWORD` | *(empty)* | Database password |
| `MCP_DB_NAME` | `mcp_db` | Database name |
| `MCP_LISTEN_ADDR` | `0.0.0.0:5353` | SSE server listen address |

## Getting Started

### 1. Set up your database

Make sure PostgreSQL is running and the `pgvector` extension is available. Create the database:

```sql
CREATE DATABASE mcp_db;
```

### 2. Configure environment

**Option A: Use a `.env` file** (recommended for local development and Docker)

```bash
cp .env.example .env
# Edit .env with your values
```

**Option B: Export environment variables directly**

Linux / macOS:
```bash
export MCP_DB_HOST=localhost
export MCP_DB_USER=postgres
export MCP_DB_PASSWORD=yourpassword
export MCP_DB_NAME=mcp_db
```

Windows (PowerShell):
```powershell
$env:MCP_DB_HOST = "localhost"
$env:MCP_DB_USER = "postgres"
$env:MCP_DB_PASSWORD = "yourpassword"
$env:MCP_DB_NAME = "mcp_db"
```

Environment variables always override `.env` file values, so you can use both — set defaults in `.env` and override specific values per environment.

### 3. Preview the schema (optional)

Print the DDL without connecting to the database:

```bash
go run main.go --init-schema
```

This outputs the SQL that creates the `documents` table, the HNSW vector index, a namespace B-tree index, and a GIN index for metadata queries.

### 4. Start the server

```bash
go run main.go
```

The server will:
1. Connect to PostgreSQL and create the schema if it doesn't exist
2. Start listening for SSE connections on `0.0.0.0:5353`

### 5. Connect an MCP client

Point your MCP client at the SSE endpoint. For example, in a Kiro or other MCP-compatible client config:

```json
{
  "mcpServers": {
    "go-postgres": {
      "url": "http://localhost:5353/sse"
    }
  }
}
```

## Database Schema

```sql
CREATE TABLE documents (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace  TEXT        NOT NULL DEFAULT 'default',
    key        TEXT        NOT NULL,
    content    TEXT        NOT NULL,
    metadata   JSONB       NOT NULL DEFAULT '{}',
    embedding  vector(384),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Indexes:
- **HNSW** on `embedding` for cosine similarity search
- **B-tree** on `namespace` for partition filtering
- **GIN** on `metadata` for JSONB containment queries

## Running Tests

Unit tests:

```bash
go test ./...
```

Integration tests require Docker (testcontainers spins up a PostgreSQL instance automatically):

```bash
go test -v -run Integration ./...
```

## Building

```bash
go build -o mcp-server .
./mcp-server
```

## License

See [LICENSE](LICENSE) for details.
