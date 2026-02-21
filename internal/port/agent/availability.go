package agent

import (
	"context"

	"github.com/google/uuid"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
)

// AgentAvailabilityReader is the narrow interface the distributor needs.
// [ISP] The distributor depends only on this one method, not the full AgentRepository.
type AgentAvailabilityReader interface {
	GetAvailable(ctx context.Context, projectID uuid.UUID, role string) ([]domainagent.Agent, error)
}
