package distributor

import (
	"context"

	"github.com/google/uuid"
)

type Assignment struct {
	TaskID  uuid.UUID `json:"task_id"`
	AgentID uuid.UUID `json:"agent_id"`
}

// Distributor selects an available agent for a given role within a project.
// [SRP] Only selects — does not notify or assign in the DB.
type Distributor interface {
	Distribute(ctx context.Context, projectID uuid.UUID, role string) (agentID uuid.UUID, err error)
}
