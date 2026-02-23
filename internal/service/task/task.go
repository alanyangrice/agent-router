package task

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"

	"github.com/google/uuid"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	portdist "github.com/alanyang/agent-mesh/internal/port/distributor"
	portbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
	portlocker "github.com/alanyang/agent-mesh/internal/port/locker"
	portnotifier "github.com/alanyang/agent-mesh/internal/port/notifier"
	porttask "github.com/alanyang/agent-mesh/internal/port/task"
	portthread "github.com/alanyang/agent-mesh/internal/port/thread"
)

// Service manages task lifecycle and drives the pipeline.
// [OCP] Pipeline routing is driven by injected pipeline.Config.
// [DIP] Depends on ports, never on adapters or transport.
type Service struct {
	repo           porttask.Repository
	bus            portbus.EventBus
	dist           portdist.Distributor
	threadRepo     portthread.Repository
	agentNotifier  portnotifier.AgentNotifier
	roleNotifier   portnotifier.RoleNotifier
	pipelineConfig pipeline.Config
	locker         portlocker.AdvisoryLocker
}

func NewService(
	repo porttask.Repository,
	bus portbus.EventBus,
	dist portdist.Distributor,
	threadRepo portthread.Repository,
	agentNotifier portnotifier.AgentNotifier,
	roleNotifier portnotifier.RoleNotifier,
	pipelineConfig pipeline.Config,
	locker portlocker.AdvisoryLocker,
) *Service {
	return &Service{
		repo:           repo,
		bus:            bus,
		dist:           dist,
		threadRepo:     threadRepo,
		agentNotifier:  agentNotifier,
		roleNotifier:   roleNotifier,
		pipelineConfig: pipelineConfig,
		locker:         locker,
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
// Pipeline routing is entirely driven by pipeline.Config — no switch statement (OCP).
func (s *Service) UpdateStatus(ctx context.Context, id uuid.UUID, from, to domaintask.Status) error {
	if !from.CanTransitionTo(to) {
		return fmt.Errorf("invalid transition from %s to %s", from, to)
	}

	if err := s.repo.UpdateStatus(ctx, id, from, to); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	s.bus.Publish(ctx, event.New(event.TypeTaskUpdated, id)) //nolint:errcheck

	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("fetch task after status update: %w", err)
	}

	// ── Bounce-back: prefer original coder, fallback to any available coder ──────────
	// Top-level, NOT inside AssignRole, because pipelineConfig[in_progress].AssignRole="".
	if to == domaintask.StatusInProgress &&
		(from == domaintask.StatusInQA || from == domaintask.StatusInReview) {

		assignedAgentID, notifyEvent := uuid.Nil, "task_returned"

		// Preferred path: atomically assign back to original coder if they are still idle.
		// AssignIfIdle uses a CTE that locks the agent row — no TOCTOU race.
		if t.CoderID != nil {
			ok, err := s.repo.AssignIfIdle(ctx, id, *t.CoderID)
			if err != nil {
				return fmt.Errorf("bounce-back preferred assign: %w", err)
			}
			if ok {
				assignedAgentID = *t.CoderID
			}
		}

		// Fallback: original coder busy/offline, or CoderID unset.
		// Role is read from config — not hardcoded ("coder").
		if assignedAgentID == uuid.Nil {
			fallbackRole := s.pipelineConfig[domaintask.StatusInProgress].FreedRole
			fallbackAgentID, err := s.dist.Distribute(ctx, t.ProjectID, fallbackRole)
			if err != nil {
				slog.Info("bounce-back: no coder available", "task_id", id)
			} else {
				assignedAgentID = fallbackAgentID
				notifyEvent = "task_assigned"
				if err := s.repo.Assign(ctx, id, assignedAgentID); err != nil {
					return fmt.Errorf("bounce-back fallback assign: %w", err)
				}
			}
		}

		if assignedAgentID != uuid.Nil {
			s.agentNotifier.NotifyAgent(ctx, assignedAgentID, map[string]string{ //nolint:errcheck
				"event": notifyEvent, "task_id": id.String(),
			})
			s.bus.Publish(ctx, event.New(event.TypeTaskAssigned, id)) //nolint:errcheck
		}

		// Sweep for the freed QA/reviewer slot — BEFORE return so it is never lost.
		if fromAction, ok := s.pipelineConfig[from]; ok {
			if role := fromAction.EffectiveFreedRole(); role != "" {
				projectID := t.ProjectID
				go func() {
					if err := s.SweepUnassigned(context.Background(), projectID, role); err != nil {
						slog.Error("sweep failed after bounce-back", "role", role, "error", err)
					}
				}()
			}
		}

		return nil // skip normal AssignRole/BroadcastEvent/TypeTaskCompleted
	}

	action, ok := s.pipelineConfig[to]
	if !ok {
		return nil
	}

	// ── AssignRole: assign task to a new agent ────────────────────────────────────────
	if action.AssignRole != "" {
		agentID, err := s.dist.Distribute(ctx, t.ProjectID, action.AssignRole)
		if err != nil {
			slog.Info("pipeline: no agent available", "task_id", id, "role", action.AssignRole)
			// Task stays unassigned — SweepUnassigned will pick it up when an agent frees.
			// Do NOT return early — still run BroadcastEvent and TypeTaskCompleted below.
		} else {
			if err := s.repo.Assign(ctx, id, agentID); err != nil {
				return fmt.Errorf("assign task to agent: %w", err)
			}
			s.agentNotifier.NotifyAgent(ctx, agentID, map[string]string{ //nolint:errcheck
				"event": "task_assigned", "task_id": id.String(),
			})
			s.bus.Publish(ctx, event.New(event.TypeTaskAssigned, id)) //nolint:errcheck
		}
	}

	// ── FreedRole sweep: fire when leaving `from` status ─────────────────────────────
	// EffectiveFreedRole() returns FreedRole if set, else falls back to AssignRole.
	// Using context.Background() so the goroutine outlives the request context.
	if fromAction, ok := s.pipelineConfig[from]; ok {
		if role := fromAction.EffectiveFreedRole(); role != "" {
			projectID := t.ProjectID
			go func() {
				if err := s.SweepUnassigned(context.Background(), projectID, role); err != nil {
					slog.Error("sweep failed after status transition", "role", role, "error", err)
				}
			}()
		}
	}

	// ── BroadcastEvent: notify a role (independent of AssignRole) ────────────────────
	if action.BroadcastEvent != "" && action.BroadcastRole != "" {
		if err := s.roleNotifier.NotifyProjectRole(ctx, t.ProjectID, action.BroadcastRole, map[string]string{
			"event":          action.BroadcastEvent,
			"merged_task_id": id.String(),
		}); err != nil {
			slog.ErrorContext(ctx, "pipeline: failed to broadcast event", "event", action.BroadcastEvent, "error", err)
		}
	}

	// ── TypeTaskCompleted: always publish on merge (independent of AssignRole) ───────
	if to == domaintask.StatusMerged {
		s.bus.Publish(ctx, event.New(event.TypeTaskCompleted, id)) //nolint:errcheck
	}

	return nil
}

// SweepUnassigned finds unassigned tasks for the given role and assigns them to
// available agents. Protected by a Postgres advisory lock so concurrent sweeps for
// the same (projectID, role) are serialised — the advisory lock prevents the race
// where two sweeps both call Distribute and pick the same agent.
func (s *Service) SweepUnassigned(ctx context.Context, projectID uuid.UUID, role string) error {
	key := advisoryKey(projectID, role)
	return s.locker.WithLock(ctx, key, func(ctx context.Context) error {
		for status, action := range s.pipelineConfig {
			// Match on AssignRole (normal assignment path) OR FreedRole (recovery path
			// for tasks stranded unassigned — e.g. in_progress after a failed bounce-back).
			if action.AssignRole != role && action.FreedRole != role {
				continue
			}
			tasks, err := s.repo.List(ctx, domaintask.ListFilters{
				ProjectID:   &projectID,
				Status:      &status,
				Unassigned:  true,
				OldestFirst: true,
			})
			if err != nil {
				return fmt.Errorf("list unassigned tasks: %w", err)
			}
			for _, t := range tasks {
				agentID, err := s.dist.Distribute(ctx, projectID, role)
				if err != nil {
					return nil // no agents left — stop, not an error
				}
				if err := s.repo.Assign(ctx, t.ID, agentID); err != nil {
					return fmt.Errorf("assign task %s: %w", t.ID, err)
				}
				s.agentNotifier.NotifyAgent(ctx, agentID, map[string]string{ //nolint:errcheck
					"event": "task_assigned", "task_id": t.ID.String(),
				})
				s.bus.Publish(ctx, event.New(event.TypeTaskAssigned, t.ID)) //nolint:errcheck
			}
		}
		return nil
	})
}

func (s *Service) SetPRUrl(ctx context.Context, id uuid.UUID, prURL string) error {
	if err := s.repo.SetPRUrl(ctx, id, prURL); err != nil {
		return fmt.Errorf("set pr_url: %w", err)
	}
	s.bus.Publish(ctx, event.New(event.TypeTaskUpdated, id)) //nolint:errcheck
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

// advisoryKey hashes (projectID, role) to a stable int64 for pg_advisory_lock.
func advisoryKey(projectID uuid.UUID, role string) int64 {
	h := fnv.New64a()
	h.Write(projectID[:])
	h.Write([]byte(role))
	return int64(h.Sum64())
}
