package task

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusBacklog    Status = "backlog"
	StatusReady      Status = "ready"
	StatusInProgress Status = "in_progress"
	StatusInQA       Status = "in_qa"
	StatusInReview   Status = "in_review"
	StatusMerged     Status = "merged"
)

var validTransitions = map[Status][]Status{
	StatusBacklog:    {StatusReady},
	StatusReady:      {StatusInProgress, StatusBacklog},
	StatusInProgress: {StatusInQA, StatusReady},
	StatusInQA:       {StatusInReview, StatusInProgress},
	StatusInReview:   {StatusMerged, StatusInProgress},
	StatusMerged:     {},
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
	ID              uuid.UUID  `json:"id"`
	ProjectID       uuid.UUID  `json:"project_id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Status          Status     `json:"status"`
	Priority        Priority   `json:"priority"`
	AssignedAgentID *uuid.UUID `json:"assigned_agent_id,omitempty"`
	CoderID         *uuid.UUID `json:"coder_id,omitempty"` // original coder — preserved for bounce-back routing
	ParentTaskID    *uuid.UUID `json:"parent_task_id,omitempty"`
	BranchType      BranchType `json:"branch_type"`
	BranchName      string     `json:"branch_name"`
	Labels          []string   `json:"labels"`
	RequiredRole    string     `json:"required_role,omitempty"`
	PRUrl           string     `json:"pr_url,omitempty"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

func New(projectID uuid.UUID, title, description string, priority Priority, branchType BranchType, createdBy string) Task {
	now := time.Now().UTC()
	return Task{
		ID:          uuid.New(),
		ProjectID:   projectID,
		Title:       title,
		Description: description,
		Status:      StatusBacklog,
		Priority:    priority,
		BranchType:  branchType,
		Labels:      []string{},
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

type Dependency struct {
	TaskID      uuid.UUID `json:"task_id"`
	DependsOnID uuid.UUID `json:"depends_on_id"`
}

type ListFilters struct {
	ProjectID   *uuid.UUID
	Status      *Status
	Priority    *Priority
	AssignedTo  *uuid.UUID
	Labels      []string
	Unassigned  bool // WHERE assigned_agent_id IS NULL
	OldestFirst bool // ORDER BY created_at ASC (default is DESC)
}
