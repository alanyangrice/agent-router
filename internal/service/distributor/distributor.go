package distributor

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	portdist "github.com/alanyang/agent-mesh/internal/port/distributor"
	portagent "github.com/alanyang/agent-mesh/internal/port/agent"
	porttask "github.com/alanyang/agent-mesh/internal/port/task"
)

var ErrNoAgentAvailable = errors.New("no agent available for task")

// compile-time check that *Service implements Distributor
var _ portdist.Distributor = (*Service)(nil)

type Service struct {
	taskRepo  porttask.Repository
	agentRepo portagent.Repository
}

func NewService(taskRepo porttask.Repository, agentRepo portagent.Repository) *Service {
	return &Service{taskRepo: taskRepo, agentRepo: agentRepo}
}

func (s *Service) Distribute(ctx context.Context, t domaintask.Task) (uuid.UUID, error) {
	agents, err := s.agentRepo.GetAvailable(ctx, t.ProjectID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("get available agents: %w", err)
	}

	if len(t.Labels) > 0 {
		var matched []agentLoad
		for _, a := range agents {
			if a.MatchesAnySkill(t.Labels) {
				matched = append(matched, agentLoad{id: a.ID})
			}
		}
		if len(matched) == 0 {
			return uuid.Nil, ErrNoAgentAvailable
		}
		return s.pickLeastLoaded(ctx, matched)
	}

	if len(agents) == 0 {
		return uuid.Nil, ErrNoAgentAvailable
	}

	loads := make([]agentLoad, len(agents))
	for i, a := range agents {
		loads[i] = agentLoad{id: a.ID}
	}
	return s.pickLeastLoaded(ctx, loads)
}

func (s *Service) Rebalance(ctx context.Context, projectID uuid.UUID) ([]portdist.Assignment, error) {
	ready, err := s.taskRepo.GetReadyTasks(ctx, projectID, nil)
	if err != nil {
		return nil, fmt.Errorf("get ready tasks: %w", err)
	}

	var assignments []portdist.Assignment
	for _, t := range ready {
		agentID, err := s.Distribute(ctx, t)
		if err != nil {
			continue
		}
		if err := s.taskRepo.Assign(ctx, t.ID, agentID); err != nil {
			continue
		}
		assignments = append(assignments, portdist.Assignment{TaskID: t.ID, AgentID: agentID})
	}

	return assignments, nil
}

type agentLoad struct {
	id    uuid.UUID
	count int
}

func (s *Service) pickLeastLoaded(ctx context.Context, candidates []agentLoad) (uuid.UUID, error) {
	if len(candidates) == 0 {
		return uuid.Nil, ErrNoAgentAvailable
	}

	// Count current tasks per candidate via the task list filtered by agent.
	for i := range candidates {
		tasks, err := s.taskRepo.List(ctx, domaintask.ListFilters{
			AssignedTo: &candidates[i].id,
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("count tasks for agent %s: %w", candidates[i].id, err)
		}
		candidates[i].count = len(tasks)
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.count < best.count {
			best = c
		}
	}
	return best.id, nil
}
