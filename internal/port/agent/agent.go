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

	UpdateStatus(ctx context.Context, id uuid.UUID, status domainagent.Status) error
	SetCurrentTask(ctx context.Context, agentID uuid.UUID, taskID *uuid.UUID) error
}
