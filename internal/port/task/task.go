package task

import (
	"context"

	"github.com/google/uuid"

	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
)

type Repository interface {
	Create(ctx context.Context, t domaintask.Task) (domaintask.Task, error)
	GetByID(ctx context.Context, id uuid.UUID) (domaintask.Task, error)
	List(ctx context.Context, filters domaintask.ListFilters) ([]domaintask.Task, error)

	// UpdateStatus performs an atomic CAS: only transitions if current status matches `from`.
	UpdateStatus(ctx context.Context, id uuid.UUID, from, to domaintask.Status) error

	// SetPRUrl records the GitHub PR URL on the task.
	SetPRUrl(ctx context.Context, id uuid.UUID, prURL string) error

	Assign(ctx context.Context, taskID, agentID uuid.UUID) error
	Unassign(ctx context.Context, taskID uuid.UUID) error
	// UnassignByAgent releases only ready tasks (status='ready') for the given agent.
	// In-flight tasks (in_progress, in_qa, in_review) are handled by the grace-period reaper.
	UnassignByAgent(ctx context.Context, agentID uuid.UUID) error

	// AssignIfIdle atomically locks the agent row, marks it working, and assigns
	// the task — all in one CTE. Returns true if the agent was idle and assignment
	// succeeded. Returns false if the agent was already claimed by a concurrent call.
	AssignIfIdle(ctx context.Context, taskID, agentID uuid.UUID) (bool, error)

	// ReleaseInFlightByAgent resets in_progress tasks to ready and unassigns
	// in_qa/in_review tasks, all in a single transaction. Returns the distinct
	// statuses that were freed so the caller can sweep the right roles.
	ReleaseInFlightByAgent(ctx context.Context, agentID uuid.UUID) ([]domaintask.Status, error)

	AddDependency(ctx context.Context, dep domaintask.Dependency) error
	RemoveDependency(ctx context.Context, taskID, dependsOnID uuid.UUID) error
	GetDependencies(ctx context.Context, taskID uuid.UUID) ([]domaintask.Task, error)
}
