package locker

import "context"

// AdvisoryLocker serialises critical sections using Postgres session advisory locks.
// WithLock ensures lock and unlock occur on the same DB connection — required for
// session-level pg_advisory_lock semantics.
type AdvisoryLocker interface {
	WithLock(ctx context.Context, key int64, fn func(ctx context.Context) error) error
}
