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
	// SetCurrentTask removed — ClaimAgent sets current_task_id atomically.

	// ClaimAgent atomically selects the oldest idle agent of the given role and
	// marks it working. Uses FOR UPDATE SKIP LOCKED and a NOT EXISTS guard to
	// prevent double-assignment. Returns ErrNoAgentAvailable when none are idle.
	ClaimAgent(ctx context.Context, projectID uuid.UUID, role string) (uuid.UUID, error)

	// SetIdleStatus returns an agent to idle with current_task_id=NULL.
	// Called by claim_task when no task is found (agent is truly free).
	SetIdleStatus(ctx context.Context, agentID uuid.UUID) error

	// SetWorkingStatus marks an agent as working on a specific task.
	// Called by claim_task as a safety net — ensures correct status even when
	// assignments arrive via bounce-back (repo.Assign) rather than ClaimAgent.
	SetWorkingStatus(ctx context.Context, agentID uuid.UUID, taskID uuid.UUID) error

	// ListOfflineWithInflightTasks returns IDs of offline agents that still have
	// in_progress, in_qa, or in_review tasks. Used on startup to reschedule
	// reaper timers lost during a process restart.
	ListOfflineWithInflightTasks(ctx context.Context) ([]uuid.UUID, error)
}
