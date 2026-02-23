package task

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	portdist "github.com/alanyang/agent-mesh/internal/port/distributor"
	portbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
	portnotifier "github.com/alanyang/agent-mesh/internal/port/notifier"
	porttask "github.com/alanyang/agent-mesh/internal/port/task"
	portthread "github.com/alanyang/agent-mesh/internal/port/thread"
)

// Service manages task lifecycle and drives the pipeline.
// [SRP] Task state management and pipeline coordination only.
// [OCP] Pipeline routing is driven by injected pipeline.Config — adding a stage
//
//	extends the config without modifying this service.
//
// [DIP] Depends on ports, never on adapters or transport.
type Service struct {
	repo           porttask.Repository
	bus            portbus.EventBus
	dist           portdist.Distributor
	threadRepo     portthread.Repository
	agentNotifier  portnotifier.AgentNotifier
	roleNotifier   portnotifier.RoleNotifier
	pipelineConfig pipeline.Config
}

func NewService(
	repo porttask.Repository,
	bus portbus.EventBus,
	dist portdist.Distributor,
	threadRepo portthread.Repository,
	agentNotifier portnotifier.AgentNotifier,
	roleNotifier portnotifier.RoleNotifier,
	pipelineConfig pipeline.Config,
) *Service {
	return &Service{
		repo:           repo,
		bus:            bus,
		dist:           dist,
		threadRepo:     threadRepo,
		agentNotifier:  agentNotifier,
		roleNotifier:   roleNotifier,
		pipelineConfig: pipelineConfig,
	}
}

func (s *Service) Create(ctx context.Context, projectID uuid.UUID, title, description string, priority domaintask.Priority, branchType domaintask.BranchType, createdBy string) (domaintask.Task, error) {
	t := domaintask.New(projectID, title, description, priority, branchType, createdBy)
	t.BranchName = fmt.Sprintf("%s/%s", branchType, t.ID.String()[:8])

	created, err := s.repo.Create(ctx, t)
	if err != nil {
		return domaintask.Task{}, fmt.Errorf("create task: %w", err)
	}

	thread := domainthread.New(projectID, domainthread.TypeTask, title, &created.ID)
	if _, err := s.threadRepo.CreateThread(ctx, thread); err != nil {
		slog.ErrorContext(ctx, "failed to auto-create task thread", "task_id", created.ID, "error", err)
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

// UpdateStatus performs a CAS status transition and drives the pipeline.
// Pipeline routing is entirely driven by pipeline.Config — no switch statement.
// [OCP] To add a pipeline stage: extend pipeline.DefaultConfig, no change here.
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

	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to fetch task for pipeline action", "task_id", id, "error", err)
		return nil
	}

	action, ok := s.pipelineConfig[to]
	if !ok {
		// No pipeline action configured for this status — nothing more to do.
		return nil
	}

	// Assign a new agent if the stage requires a specific role.
	if action.AssignRole != "" {
		agentID, err := s.dist.Distribute(ctx, t.ProjectID, action.AssignRole)
		if err != nil {
			slog.InfoContext(ctx, "pipeline: no agent available for role", "task_id", id, "role", action.AssignRole)
			// Task stays in the new status but unassigned. MCP claim_task will pick it up.
			return nil
		}

		// Ownership lock: when bouncing back to in_progress, preserve the existing coder.
		// Only assign a new agent if either there is no current assignment OR the stage
		// explicitly needs a different role (e.g. in_qa → qa, in_review → reviewer).
		if to == domaintask.StatusInProgress && t.AssignedAgentID != nil {
			// Keep the existing coder assigned — notify them.
			if err := s.agentNotifier.NotifyAgent(ctx, *t.AssignedAgentID, map[string]string{
				"event":   "task_assigned",
				"task_id": id.String(),
			}); err != nil {
				slog.ErrorContext(ctx, "pipeline: failed to notify coder of bounce-back", "agent_id", *t.AssignedAgentID, "error", err)
			}
		} else {
			if err := s.repo.Assign(ctx, id, agentID); err != nil {
				slog.ErrorContext(ctx, "pipeline: failed to assign task", "task_id", id, "agent_id", agentID, "error", err)
				return nil
			}
			if err := s.agentNotifier.NotifyAgent(ctx, agentID, map[string]string{
				"event":   "task_assigned",
				"task_id": id.String(),
			}); err != nil {
				slog.ErrorContext(ctx, "pipeline: failed to notify assigned agent", "agent_id", agentID, "error", err)
			}
			if err := s.bus.Publish(ctx, event.New(event.TypeTaskAssigned, id)); err != nil {
				slog.ErrorContext(ctx, "failed to publish TaskAssigned event", "task_id", id, "error", err)
			}
		}
	}

	// Broadcast a notification to all agents of a specific role (e.g. main_updated to coders).
	if action.BroadcastEvent != "" {
		if err := s.roleNotifier.NotifyProjectRole(ctx, t.ProjectID, "coder", map[string]string{
			"event":          action.BroadcastEvent,
			"merged_task_id": id.String(),
		}); err != nil {
			slog.ErrorContext(ctx, "pipeline: failed to broadcast event", "event", action.BroadcastEvent, "error", err)
		}
	}

	if to == domaintask.StatusMerged {
		if err := s.bus.Publish(ctx, event.New(event.TypeTaskCompleted, id)); err != nil {
			slog.ErrorContext(ctx, "failed to publish TaskCompleted event", "task_id", id, "error", err)
		}
	}

	return nil
}

func (s *Service) SetPRUrl(ctx context.Context, id uuid.UUID, prURL string) error {
	if err := s.repo.SetPRUrl(ctx, id, prURL); err != nil {
		return fmt.Errorf("set pr_url: %w", err)
	}
	if err := s.bus.Publish(ctx, event.New(event.TypeTaskUpdated, id)); err != nil {
		slog.ErrorContext(ctx, "failed to publish TaskUpdated after pr_url set", "task_id", id, "error", err)
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
