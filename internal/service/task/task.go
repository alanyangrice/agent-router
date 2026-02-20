package task

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	portagent "github.com/alanyang/agent-mesh/internal/port/distributor"
	portbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
	porttask "github.com/alanyang/agent-mesh/internal/port/task"
)

type Service struct {
	repo porttask.Repository
	bus  portbus.EventBus
	dist portagent.Distributor
}

func NewService(repo porttask.Repository, bus portbus.EventBus, dist portagent.Distributor) *Service {
	return &Service{repo: repo, bus: bus, dist: dist}
}

func (s *Service) Create(ctx context.Context, projectID uuid.UUID, title, description string, priority domaintask.Priority, branchType domaintask.BranchType, createdBy string) (domaintask.Task, error) {
	t := domaintask.New(projectID, title, description, priority, branchType, createdBy)
	t.BranchName = fmt.Sprintf("%s/%s", branchType, t.ID.String()[:8])

	created, err := s.repo.Create(ctx, t)
	if err != nil {
		return domaintask.Task{}, fmt.Errorf("create task: %w", err)
	}

	if err := s.bus.Publish(ctx, event.New(event.TypeTaskCreated, created.ID)); err != nil {
		slog.ErrorContext(ctx, "failed to publish TaskCreated event", "task_id", created.ID, "error", err)
	}

	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (domaintask.Task, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domaintask.Task{}, fmt.Errorf("get task: %w", err)
	}
	return t, nil
}

func (s *Service) List(ctx context.Context, filters domaintask.ListFilters) ([]domaintask.Task, error) {
	tasks, err := s.repo.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, nil
}

func (s *Service) UpdateStatus(ctx context.Context, id uuid.UUID, from, to domaintask.Status) error {
	if !from.CanTransitionTo(to) {
		return fmt.Errorf("invalid transition from %s to %s", from, to)
	}

	if err := s.repo.UpdateStatus(ctx, id, from, to); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	if err := s.bus.Publish(ctx, event.New(event.TypeTaskUpdated, id)); err != nil {
		slog.ErrorContext(ctx, "failed to publish TaskUpdated event", "task_id", id, "error", err)
	}

	switch to {
	case domaintask.StatusReady:
		t, err := s.repo.GetByID(ctx, id)
		if err != nil {
			slog.ErrorContext(ctx, "failed to fetch task for auto-distribute", "task_id", id, "error", err)
			break
		}
		agentID, err := s.dist.Distribute(ctx, t)
		if err != nil {
			slog.InfoContext(ctx, "auto-distribute found no agent", "task_id", id, "error", err)
			break
		}
		if err := s.repo.Assign(ctx, id, agentID); err != nil {
			slog.ErrorContext(ctx, "failed to assign task after distribute", "task_id", id, "agent_id", agentID, "error", err)
			break
		}
		if err := s.bus.Publish(ctx, event.New(event.TypeTaskAssigned, id)); err != nil {
			slog.ErrorContext(ctx, "failed to publish TaskAssigned event", "task_id", id, "error", err)
		}

	case domaintask.StatusDone:
		if err := s.bus.Publish(ctx, event.New(event.TypeTaskCompleted, id)); err != nil {
			slog.ErrorContext(ctx, "failed to publish TaskCompleted event", "task_id", id, "error", err)
		}
	}

	return nil
}

func (s *Service) AddDependency(ctx context.Context, taskID, dependsOnID uuid.UUID) error {
	dep := domaintask.Dependency{TaskID: taskID, DependsOnID: dependsOnID}
	if err := s.repo.AddDependency(ctx, dep); err != nil {
		return fmt.Errorf("add dependency: %w", err)
	}
	return nil
}

func (s *Service) RemoveDependency(ctx context.Context, taskID, dependsOnID uuid.UUID) error {
	if err := s.repo.RemoveDependency(ctx, taskID, dependsOnID); err != nil {
		return fmt.Errorf("remove dependency: %w", err)
	}
	return nil
}

func (s *Service) GetDependencies(ctx context.Context, taskID uuid.UUID) ([]domaintask.Task, error) {
	tasks, err := s.repo.GetDependencies(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get dependencies: %w", err)
	}
	return tasks, nil
}
