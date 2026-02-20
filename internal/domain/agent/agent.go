package agent

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusIdle    Status = "idle"
	StatusWorking Status = "working"
	StatusBlocked Status = "blocked"
	StatusOffline Status = "offline"
)

type Agent struct {
	ID              uuid.UUID              `json:"id"`
	ProjectID       uuid.UUID              `json:"project_id"`
	Role            string                 `json:"role"`
	Name            string                 `json:"name"`
	Skills          []string               `json:"skills"`
	Model           string                 `json:"model"`
	Status          Status                 `json:"status"`
	CurrentTaskID   *uuid.UUID             `json:"current_task_id,omitempty"`
	Config          map[string]interface{} `json:"config,omitempty"`
	Stats           map[string]interface{} `json:"stats,omitempty"`
	LastHeartbeatAt *time.Time             `json:"last_heartbeat_at,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
}

func New(projectID uuid.UUID, role, name, model string, skills []string) Agent {
	now := time.Now().UTC()
	return Agent{
		ID:        uuid.New(),
		ProjectID: projectID,
		Role:      role,
		Name:      name,
		Skills:    skills,
		Model:     model,
		Status:    StatusIdle,
		Config:    map[string]interface{}{},
		Stats:     map[string]interface{}{},
		CreatedAt: now,
	}
}

func (a *Agent) RecordHeartbeat() {
	now := time.Now().UTC()
	a.LastHeartbeatAt = &now
}

func (a *Agent) IsStale(timeout time.Duration) bool {
	if a.LastHeartbeatAt == nil {
		return true
	}
	return time.Since(*a.LastHeartbeatAt) > timeout
}

func (a *Agent) HasSkill(skill string) bool {
	for _, s := range a.Skills {
		if s == skill {
			return true
		}
	}
	return false
}

func (a *Agent) MatchesAnySkill(required []string) bool {
	for _, req := range required {
		if a.HasSkill(req) {
			return true
		}
	}
	return false
}

type ListFilters struct {
	ProjectID *uuid.UUID
	Role      *string
	Status    *Status
}
