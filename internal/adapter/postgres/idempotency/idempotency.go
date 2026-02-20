package idempotency

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Check looks up an existing idempotency key. Returns the stored result JSON,
// whether the key exists, and any error.
func (r *Repository) Check(ctx context.Context, key string) ([]byte, bool, error) {
	query := `SELECT result_jsonb FROM processed_operations WHERE idempotency_key = $1`

	var result []byte
	err := r.pool.QueryRow(ctx, query, key).Scan(&result)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checking idempotency key: %w", err)
	}
	return result, true, nil
}

// Store records a processed operation keyed by the idempotency key.
func (r *Repository) Store(ctx context.Context, key string, agentID uuid.UUID, opType string, resultJSON []byte) error {
	query := `
		INSERT INTO processed_operations (idempotency_key, agent_id, operation_type, result_jsonb, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (idempotency_key) DO NOTHING`

	_, err := r.pool.Exec(ctx, query, key, agentID, opType, resultJSON)
	if err != nil {
		return fmt.Errorf("storing idempotency key: %w", err)
	}
	return nil
}
