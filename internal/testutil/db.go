//go:build integration

package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SetupTestDB connects to the test database and applies both migrations.
// It skips the test if TEST_DATABASE_URL is not set.
// Each call uses the same DB — callers must scope isolation by unique project_id.
func SetupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping test DB: %v", err)
	}

	applyMigrations(t, pool)

	t.Cleanup(func() { pool.Close() })
	return pool
}

func applyMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	// Read and apply migration files. Errors are non-fatal if objects already exist.
	migrations := []string{
		"internal/adapter/postgres/migrations/001_initial.sql",
		"internal/adapter/postgres/migrations/002_v2.sql",
	}
	for _, path := range migrations {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Logf("migration file %s not found, skipping: %v", path, err)
			continue
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			// Migrations may fail if already applied — log and continue.
			t.Logf("migration %s: %v (may already be applied)", path, err)
		}
	}
}
