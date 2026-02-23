package distributor

import (
	"context"
	"errors"

	"github.com/google/uuid"

	portagent "github.com/alanyang/agent-mesh/internal/port/agent"
	portdist "github.com/alanyang/agent-mesh/internal/port/distributor"
)

var ErrNoAgentAvailable = errors.New("no agent available for role")

var _ portdist.Distributor = (*Service)(nil)

// Service selects and atomically claims the best available agent for a given role.
// ClaimAgent is atomic (UPDATE...RETURNING with FOR UPDATE SKIP LOCKED + NOT EXISTS guard),
// so two concurrent Distribute calls can never claim the same agent.
type Service struct {
	agentRepo portagent.Repository
}

func NewService(agentRepo portagent.Repository) *Service {
	return &Service{agentRepo: agentRepo}
}

// Distribute atomically claims the oldest idle agent with the given role.
func (s *Service) Distribute(ctx context.Context, projectID uuid.UUID, role string) (uuid.UUID, error) {
	return s.agentRepo.ClaimAgent(ctx, projectID, role)
}
