package task

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusBacklog    Status = "backlog"
	StatusReady      Status = "ready"
	StatusInProgress Status = "in_progress"
	StatusInReview   Status = "in_review"
	StatusDone       Status = "done"
)

var validTransitions = map[Status][]Status{
	StatusBacklog:    {StatusReady},
	StatusReady:      {StatusInProgress, StatusBacklog},
	StatusInProgress: {StatusInReview, StatusReady},
	StatusInReview:   {StatusDone, StatusInProgress},
	StatusDone:       {},
}

func (s Status) CanTransitionTo(target Status) bool {
	for _, allowed := range validTransitions[s] {
		if allowed == target {
			return true
		}
	}
	return false
}

type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityMedium   Priority = "medium"
	PriorityLow      Priority = "low"
)

type BranchType string

const (
	BranchFeature  BranchType = "feature"
	BranchFix      BranchType = "fix"
	BranchRefactor BranchType = "refactor"
)

type Task struct {
	ID             uuid.UUID  `json:"id"`
	ProjectID      uuid.UUID  `json:"project_id"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Status         Status     `json:"status"`
	Priority       Priority   `json:"priority"`
	AssignedAgentID *uuid.UUID `json:"assigned_agent_id,omitempty"`
	ParentTaskID   *uuid.UUID `json:"parent_task_id,omitempty"`
	BranchType     BranchType `json:"branch_type"`
	BranchName     string     `json:"branch_name"`
	Labels         []string   `json:"labels"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

func New(projectID uuid.UUID, title, description string, priority Priority, branchType BranchType, createdBy string) Task {
	now := time.Now().UTC()
	return Task{
		ID:         uuid.New(),
		ProjectID:  projectID,
		Title:      title,
		Description: description,
		Status:     StatusBacklog,
		Priority:   priority,
		BranchType: branchType,
		Labels:     []string{},
		CreatedBy:  createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func (t *Task) TransitionTo(target Status) error {
	if !t.Status.CanTransitionTo(target) {
		return fmt.Errorf("invalid transition from %s to %s", t.Status, target)
	}

	now := time.Now().UTC()
	t.Status = target
	t.UpdatedAt = now

	switch target {
	case StatusInProgress:
		t.StartedAt = &now
	case StatusDone:
		t.CompletedAt = &now
	}

	return nil
}

type Dependency struct {
	TaskID      uuid.UUID `json:"task_id"`
	DependsOnID uuid.UUID `json:"depends_on_id"`
}

type ListFilters struct {
	ProjectID  *uuid.UUID
	Status     *Status
	Priority   *Priority
	AssignedTo *uuid.UUID
	Labels     []string
}
