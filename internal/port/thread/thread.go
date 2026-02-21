package thread

import (
	"context"

	"github.com/google/uuid"

	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
)

// Repository is the storage abstraction for threads and messages.
// [ISP] Visibility methods removed — no consumer calls them.
type Repository interface {
	CreateThread(ctx context.Context, t domainthread.Thread) (domainthread.Thread, error)
	GetThreadByID(ctx context.Context, id uuid.UUID) (domainthread.Thread, error)
	ListThreads(ctx context.Context, filters domainthread.ListFilters) ([]domainthread.Thread, error)

	CreateMessage(ctx context.Context, m domainthread.Message) (domainthread.Message, error)
	ListMessages(ctx context.Context, threadID uuid.UUID) ([]domainthread.Message, error)
}
