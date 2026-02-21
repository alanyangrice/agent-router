package distributor

import (
	"context"

	"github.com/google/uuid"
)

// Distributor selects an available agent for a given role within a project.
// [SRP] Only selects — does not notify or assign in the DB.
type Distributor interface {
	Distribute(ctx context.Context, projectID uuid.UUID, role string) (agentID uuid.UUID, err error)
}
