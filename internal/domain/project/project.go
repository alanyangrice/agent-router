package project

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID        uuid.UUID              `json:"id"`
	Name      string                 `json:"name"`
	RepoURL   string                 `json:"repo_url"`
	Config    map[string]interface{} `json:"config,omitempty"`
	Spec      string                 `json:"spec,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}
