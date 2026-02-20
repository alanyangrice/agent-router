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
	Update(ctx context.Context, t domaintask.Task) error

	// UpdateStatus performs an atomic CAS: only transitions if current status matches `from`.
	UpdateStatus(ctx context.Context, id uuid.UUID, from, to domaintask.Status) error

	Assign(ctx context.Context, taskID, agentID uuid.UUID) error
	Unassign(ctx context.Context, taskID uuid.UUID) error

	AddDependency(ctx context.Context, dep domaintask.Dependency) error
	RemoveDependency(ctx context.Context, taskID, dependsOnID uuid.UUID) error
	GetDependencies(ctx context.Context, taskID uuid.UUID) ([]domaintask.Task, error)

	// GetReadyTasks returns tasks in "ready" status whose dependencies are all "done",
	// optionally filtered by required skills.
	GetReadyTasks(ctx context.Context, projectID uuid.UUID, skills []string) ([]domaintask.Task, error)
}
