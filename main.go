package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-mcp-postgres-server/config"
	"go-mcp-postgres-server/db"
	"go-mcp-postgres-server/tools"

	"github.com/mark3labs/mcp-go/server"
)

const serverVersion = "1.0.0"

func main() {
	// 1. Parse CLI flags
	initSchema := flag.Bool("init-schema", false, "Print schema DDL and exit")
	flag.Parse()

	// 2. Initialize structured JSON logger
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// 3. Load configuration
	cfg := config.Load()

	// 4. If --init-schema: print DDL and exit without connecting to DB
	if *initSchema {
		db.PrintSchema(os.Stdout)
		os.Exit(0)
	}

	// 5. Log startup info
	slog.Info("starting server",
		"version", serverVersion,
		"listen_addr", cfg.ListenAddr,
		"db_host", cfg.DBHost,
	)
	slog.Info("active configuration", "config", cfg.LogSafe())

	// 6. Create connection pool
	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DSN())
	if err != nil {
		slog.Error("failed to create database connection pool", "error", err)
		os.Exit(1)
	}

	// 7. Run schema initialization
	if err := db.InitSchema(ctx, pool); err != nil {
		slog.Error("failed to initialize database schema", "error", err)
		os.Exit(1)
	}

	// 8. Create repository
	repo := db.NewRepository(pool)

	// 9. Create MCP server with panic recovery
	mcpServer := server.NewMCPServer("go-mcp-postgres-server", serverVersion,
		server.WithRecovery(),
	)

	// 10. Register all 6 tools
	mcpServer.AddTool(tools.NewStoreTool(), tools.StoreHandler(repo))
	mcpServer.AddTool(tools.NewQueryTool(), tools.QueryHandler(repo))
	mcpServer.AddTool(tools.NewGetTool(), tools.GetHandler(repo))
	mcpServer.AddTool(tools.NewListTool(), tools.ListHandler(repo))
	mcpServer.AddTool(tools.NewUpdateTool(), tools.UpdateHandler(repo))
	mcpServer.AddTool(tools.NewDeleteTool(), tools.DeleteHandler(repo))

	// 11. Create and start SSE server
	sseServer := server.NewSSEServer(mcpServer)

	go func() {
		slog.Info("SSE server listening", "addr", cfg.ListenAddr)
		if err := sseServer.Start(cfg.ListenAddr); err != nil {
			slog.Error("SSE server error", "error", err)
			os.Exit(1)
		}
	}()

	// 12. Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	slog.Info("received shutdown signal", "signal", sig.String())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := sseServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("SSE server shutdown error", "error", err)
	}

	pool.Close()

	slog.Info("shutdown complete")
}
