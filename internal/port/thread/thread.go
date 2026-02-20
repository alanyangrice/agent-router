package thread

import (
	"context"

	"github.com/google/uuid"

	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
)

type Repository interface {
	CreateThread(ctx context.Context, t domainthread.Thread) (domainthread.Thread, error)
	GetThreadByID(ctx context.Context, id uuid.UUID) (domainthread.Thread, error)
	ListThreads(ctx context.Context, filters domainthread.ListFilters) ([]domainthread.Thread, error)

	CreateMessage(ctx context.Context, m domainthread.Message) (domainthread.Message, error)
	ListMessages(ctx context.Context, threadID uuid.UUID) ([]domainthread.Message, error)

	SetVisibility(ctx context.Context, v domainthread.Visibility) error
	GetVisibleRoles(ctx context.Context, threadID uuid.UUID) ([]string, error)

	// IsVisibleTo checks if a thread is visible to the given agent role.
	IsVisibleTo(ctx context.Context, threadID uuid.UUID, agentRole string) (bool, error)
}
