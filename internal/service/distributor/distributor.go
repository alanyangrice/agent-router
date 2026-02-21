package distributor

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	portagentavail "github.com/alanyang/agent-mesh/internal/port/agent"
	portdist "github.com/alanyang/agent-mesh/internal/port/distributor"
)

var ErrNoAgentAvailable = errors.New("no agent available for role")

var _ portdist.Distributor = (*Service)(nil)

// Service selects the best available agent for a given role.
// [SRP] Only selects agents — no notification responsibility.
// [ISP] Depends on AgentAvailabilityReader (1 method), not the full AgentRepository.
type Service struct {
	agentAvail portagentavail.AgentAvailabilityReader
}

func NewService(agentAvail portagentavail.AgentAvailabilityReader) *Service {
	return &Service{agentAvail: agentAvail}
}

// Distribute selects the first available idle agent with the given role in the project.
func (s *Service) Distribute(ctx context.Context, projectID uuid.UUID, role string) (uuid.UUID, error) {
	agents, err := s.agentAvail.GetAvailable(ctx, projectID, role)
	if err != nil {
		return uuid.Nil, fmt.Errorf("get available agents for role %q: %w", role, err)
	}
	if len(agents) == 0 {
		return uuid.Nil, ErrNoAgentAvailable
	}
	// GetAvailable orders by created_at ASC — simple FIFO across agents of the same role.
	return agents[0].ID, nil
}
