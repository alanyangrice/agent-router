package distributor

import (
	"context"

	"github.com/google/uuid"

	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
)

type Assignment struct {
	TaskID  uuid.UUID `json:"task_id"`
	AgentID uuid.UUID `json:"agent_id"`
}

type Distributor interface {
	// Distribute selects the best available agent for the given task.
	Distribute(ctx context.Context, t domaintask.Task) (agentID uuid.UUID, err error)

	// Rebalance reviews all in-progress assignments and rebalances if needed.
	Rebalance(ctx context.Context, projectID uuid.UUID) ([]Assignment, error)
}
