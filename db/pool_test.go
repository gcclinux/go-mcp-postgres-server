package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestNewPool_UnreachableDB verifies that NewPool returns a descriptive error
// with a "db:" prefix when the target PostgreSQL host is unreachable.
// Validates: Requirements 2.3
func TestNewPool_UnreachableDB(t *testing.T) {
	// Use a short timeout so the test doesn't hang waiting for a connection.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Point at a non-existent host on a high port that nothing is listening on.
	dsn := "postgres://user:pass@127.0.0.1:59999/nonexistent?sslmode=disable"

	pool, err := NewPool(ctx, dsn)
	if pool != nil {
		pool.Close()
		t.Fatal("expected nil pool when DB is unreachable, got non-nil")
	}
	if err == nil {
		t.Fatal("expected non-nil error when DB is unreachable")
	}
	if !strings.HasPrefix(err.Error(), "db:") {
		t.Errorf("expected error to start with \"db:\" prefix, got: %s", err.Error())
	}
}

// TestNewPool_InvalidDSN verifies that NewPool returns a non-nil error when
// given a malformed DSN string.
// Validates: Requirements 2.3
func TestNewPool_InvalidDSN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, "not-a-valid-dsn://???")
	if pool != nil {
		pool.Close()
		t.Fatal("expected nil pool for invalid DSN, got non-nil")
	}
	if err == nil {
		t.Fatal("expected non-nil error for invalid DSN")
	}
	if !strings.HasPrefix(err.Error(), "db:") {
		t.Errorf("expected error to start with \"db:\" prefix, got: %s", err.Error())
	}
}
