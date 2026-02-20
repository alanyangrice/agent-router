package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	"github.com/alanyang/agent-mesh/internal/domain/event"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	portagent "github.com/alanyang/agent-mesh/internal/port/agent"
	portbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
	porttask "github.com/alanyang/agent-mesh/internal/port/task"
)

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

func (s *Service) Heartbeat(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.UpdateHeartbeat(ctx, id); err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}

	if err := s.bus.Publish(ctx, event.New(event.TypeAgentHeartbeat, id)); err != nil {
		slog.ErrorContext(ctx, "failed to publish AgentHeartbeat event", "agent_id", id, "error", err)
	}

	return nil
}

func (s *Service) MarkOffline(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.UpdateStatus(ctx, id, domainagent.StatusOffline); err != nil {
		return fmt.Errorf("mark agent offline: %w", err)
	}

	if err := s.bus.Publish(ctx, event.New(event.TypeAgentOffline, id)); err != nil {
		slog.ErrorContext(ctx, "failed to publish AgentOffline event", "agent_id", id, "error", err)
	}

	return nil
}

// StartReaper runs a background goroutine that periodically detects stale
// agents and marks them offline, resetting their assigned tasks to "ready".
func (s *Service) StartReaper(ctx context.Context, thresholdSeconds int) {
	ticker := time.NewTicker(time.Duration(thresholdSeconds) * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.InfoContext(ctx, "agent reaper stopped")
				return
			case <-ticker.C:
				s.reap(ctx, thresholdSeconds)
			}
		}
	}()
}

func (s *Service) reap(ctx context.Context, thresholdSeconds int) {
	stale, err := s.repo.GetStale(ctx, thresholdSeconds)
	if err != nil {
		slog.ErrorContext(ctx, "reaper: failed to get stale agents", "error", err)
		return
	}

	for _, a := range stale {
		if err := s.repo.UpdateStatus(ctx, a.ID, domainagent.StatusOffline); err != nil {
			slog.ErrorContext(ctx, "reaper: failed to mark agent offline", "agent_id", a.ID, "error", err)
			continue
		}

		if a.CurrentTaskID != nil {
			if err := s.taskRepo.UpdateStatus(ctx, *a.CurrentTaskID, domaintask.StatusInProgress, domaintask.StatusReady); err != nil {
				slog.ErrorContext(ctx, "reaper: failed to reset task to ready", "task_id", *a.CurrentTaskID, "error", err)
			}
			if err := s.taskRepo.Unassign(ctx, *a.CurrentTaskID); err != nil {
				slog.ErrorContext(ctx, "reaper: failed to unassign task", "task_id", *a.CurrentTaskID, "error", err)
			}
			if err := s.repo.SetCurrentTask(ctx, a.ID, nil); err != nil {
				slog.ErrorContext(ctx, "reaper: failed to clear agent current task", "agent_id", a.ID, "error", err)
			}
		}

		if err := s.bus.Publish(ctx, event.New(event.TypeAgentOffline, a.ID)); err != nil {
			slog.ErrorContext(ctx, "reaper: failed to publish AgentOffline event", "agent_id", a.ID, "error", err)
		}

		slog.InfoContext(ctx, "reaper: marked agent offline", "agent_id", a.ID)
	}
}
