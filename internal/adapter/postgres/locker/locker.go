package locker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Locker implements port/locker.AdvisoryLocker using Postgres session advisory locks.
// All lock/unlock operations occur on the same acquired connection, which is required
// because pg_advisory_lock is session-level — unlock on a different connection is a no-op.
type Locker struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Locker {
	return &Locker{pool: pool}
}

func (l *Locker) WithLock(ctx context.Context, key int64, fn func(ctx context.Context) error) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for advisory lock: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	// Unlock on the same connection before releasing it back to the pool.
	// context.Background() ensures unlock fires even if ctx was cancelled mid-fn.
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", key) //nolint:errcheck

	return fn(ctx)
}
