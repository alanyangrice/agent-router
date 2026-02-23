package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	"github.com/alanyang/agent-mesh/internal/domain/event"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	portagent "github.com/alanyang/agent-mesh/internal/port/agent"
	portbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
	porttask "github.com/alanyang/agent-mesh/internal/port/task"
)

// Service manages agent lifecycle: registration, status, and orphan recovery.
// [SRP] Agent lifecycle only. Reaper scheduling is the MCP server's responsibility.
type Service struct {
	repo     portagent.Repository
	taskRepo porttask.Repository
	bus      portbus.EventBus
}

func NewService(repo portagent.Repository, taskRepo porttask.Repository, bus portbus.EventBus) *Service {
	return &Service{repo: repo, taskRepo: taskRepo, bus: bus}
}

func (s *Service) Register(ctx context.Context, projectID uuid.UUID, role, name, model string, skills []string) (domainagent.Agent, error) {
	a := domainagent.New(projectID, role, name, model, skills)

	created, err := s.repo.Create(ctx, a)
	if err != nil {
		return domainagent.Agent{}, fmt.Errorf("register agent: %w", err)
	}

	if err := s.bus.Publish(ctx, event.New(event.TypeAgentOnline, created.ID)); err != nil {
		slog.ErrorContext(ctx, "failed to publish AgentOnline event", "agent_id", created.ID, "error", err)
	}

	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (domainagent.Agent, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domainagent.Agent{}, fmt.Errorf("get agent: %w", err)
	}
	return a, nil
}

func (s *Service) List(ctx context.Context, filters domainagent.ListFilters) ([]domainagent.Agent, error) {
	agents, err := s.repo.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return agents, nil
}

// Reactivate marks an existing agent as idle and publishes TypeAgentOnline.
// Called when an agent reconnects with a previously-issued agent_id.
// Returns an error if the agent does not exist — the caller should fall back to Register.
func (s *Service) Reactivate(ctx context.Context, agentID uuid.UUID) (domainagent.Agent, error) {
	a, err := s.repo.GetByID(ctx, agentID)
	if err != nil {
		return domainagent.Agent{}, fmt.Errorf("agent not found: %w", err)
	}
	if err := s.repo.UpdateStatus(ctx, agentID, domainagent.StatusIdle); err != nil {
		return domainagent.Agent{}, fmt.Errorf("reactivate agent: %w", err)
	}
	a.Status = domainagent.StatusIdle
	if err := s.bus.Publish(ctx, event.New(event.TypeAgentOnline, agentID)); err != nil {
		slog.ErrorContext(ctx, "failed to publish AgentOnline on reactivate", "agent_id", agentID, "error", err)
	}
	return a, nil
}

// ReapOrphaned is called by the MCP server when an agent's SSE session closes.
// It marks the agent offline and recovers any orphaned tasks.
// [SRP] This is the agent service's only lifecycle responsibility outside registration.
func (s *Service) ReapOrphaned(ctx context.Context, agentID uuid.UUID) {
	a, err := s.repo.GetByID(ctx, agentID)
	if err != nil {
		slog.ErrorContext(ctx, "reap: agent not found", "agent_id", agentID, "error", err)
		return
	}

	if err := s.repo.UpdateStatus(ctx, agentID, domainagent.StatusOffline); err != nil {
		slog.ErrorContext(ctx, "reap: failed to mark agent offline", "agent_id", agentID, "error", err)
		return
	}

	// Reset any in-progress task back to ready so another agent can pick it up.
	if a.CurrentTaskID != nil {
		if err := s.taskRepo.UpdateStatus(ctx, *a.CurrentTaskID, domaintask.StatusInProgress, domaintask.StatusReady); err != nil {
			slog.ErrorContext(ctx, "reap: failed to reset in-progress task", "task_id", *a.CurrentTaskID, "error", err)
		}
		if err := s.taskRepo.Unassign(ctx, *a.CurrentTaskID); err != nil {
			slog.ErrorContext(ctx, "reap: failed to unassign task", "task_id", *a.CurrentTaskID, "error", err)
		}
		if err := s.repo.SetCurrentTask(ctx, agentID, nil); err != nil {
			slog.ErrorContext(ctx, "reap: failed to clear current task", "agent_id", agentID, "error", err)
		}
	}

	// Also release any ready tasks that were assigned but never started.
	if err := s.taskRepo.UnassignByAgent(ctx, agentID); err != nil {
		slog.ErrorContext(ctx, "reap: failed to unassign ready tasks", "agent_id", agentID, "error", err)
	}

	if err := s.bus.Publish(ctx, event.New(event.TypeAgentOffline, agentID)); err != nil {
		slog.ErrorContext(ctx, "reap: failed to publish AgentOffline event", "agent_id", agentID, "error", err)
	}

	slog.InfoContext(ctx, "reap: agent session closed, tasks recovered", "agent_id", agentID)
}
