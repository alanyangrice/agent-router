package agent

import (
	"context"

	"github.com/google/uuid"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
)

// Repository manages agent state in the database.
type Repository interface {
	Create(ctx context.Context, a domainagent.Agent) (domainagent.Agent, error)
	GetByID(ctx context.Context, id uuid.UUID) (domainagent.Agent, error)
	List(ctx context.Context, filters domainagent.ListFilters) ([]domainagent.Agent, error)
	Update(ctx context.Context, a domainagent.Agent) error
	Delete(ctx context.Context, id uuid.UUID) error

	UpdateHeartbeat(ctx context.Context, id uuid.UUID) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status domainagent.Status) error
	SetCurrentTask(ctx context.Context, agentID uuid.UUID, taskID *uuid.UUID) error

	// GetStale returns agents whose last heartbeat exceeds the given threshold (seconds).
	GetStale(ctx context.Context, thresholdSeconds int) ([]domainagent.Agent, error)
}
