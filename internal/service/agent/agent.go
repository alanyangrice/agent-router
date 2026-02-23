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
// It marks the agent offline and releases only ready tasks (assigned but never started).
// In-flight tasks (in_progress, in_qa, in_review) are handled by the grace-period timer
// in wire/app.go via ReleaseAgent — giving the agent a window to reconnect first.
func (s *Service) ReapOrphaned(ctx context.Context, agentID uuid.UUID) {
	if err := s.repo.UpdateStatus(ctx, agentID, domainagent.StatusOffline); err != nil {
		slog.ErrorContext(ctx, "reap: failed to mark agent offline", "agent_id", agentID, "error", err)
		return
	}

	// Release only ready tasks — in-flight tasks are preserved for the grace period.
	if err := s.taskRepo.UnassignByAgent(ctx, agentID); err != nil {
		slog.ErrorContext(ctx, "reap: failed to release ready tasks", "agent_id", agentID, "error", err)
	}

	if err := s.bus.Publish(ctx, event.New(event.TypeAgentOffline, agentID)); err != nil {
		slog.ErrorContext(ctx, "reap: failed to publish AgentOffline", "agent_id", agentID, "error", err)
	}

	slog.InfoContext(ctx, "reap: agent session closed", "agent_id", agentID)
}

// SetIdle marks an agent idle with no current task.
// Called from claim_task when it returns null — the agent has no work to do.
func (s *Service) SetIdle(ctx context.Context, agentID uuid.UUID) {
	if err := s.repo.SetIdleStatus(ctx, agentID); err != nil {
		slog.ErrorContext(ctx, "set idle: failed", "agent_id", agentID, "error", err)
	}
}

// SetWorking marks an agent as working on a specific task.
// Called from claim_task as a safety net — ensures correct status even when assignments
// arrive via bounce-back (repo.Assign) rather than through ClaimAgent.
func (s *Service) SetWorking(ctx context.Context, agentID, taskID uuid.UUID) {
	if err := s.repo.SetWorkingStatus(ctx, agentID, taskID); err != nil {
		slog.ErrorContext(ctx, "set working: failed", "agent_id", agentID, "task_id", taskID, "error", err)
	}
}

// ReleaseAgent is called by the grace-period reaper after the timer expires.
// It checks the agent is still offline (guard against reconnect during grace period),
// then releases in-flight tasks and returns the freed statuses and the agent's projectID
// so the caller can sweep the correct roles without a second GetByID call.
func (s *Service) ReleaseAgent(ctx context.Context, agentID uuid.UUID) (uuid.UUID, []domaintask.Status, error) {
	a, err := s.repo.GetByID(ctx, agentID)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("release: agent not found: %w", err)
	}
	if a.Status != domainagent.StatusOffline {
		// Agent reconnected during grace period — nothing to release.
		return uuid.Nil, nil, nil
	}
	freed, err := s.taskRepo.ReleaseInFlightByAgent(ctx, agentID)
	return a.ProjectID, freed, err
}

// ListOfflineWithInflightTasks returns IDs of offline agents that still have in-flight tasks.
// Used on startup to reschedule reaper timers lost during a process restart.
func (s *Service) ListOfflineWithInflightTasks(ctx context.Context) ([]uuid.UUID, error) {
	return s.repo.ListOfflineWithInflightTasks(ctx)
}
