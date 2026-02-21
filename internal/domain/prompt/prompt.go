package prompt

import (
	"time"

	"github.com/google/uuid"
)

// RolePrompt stores the system prompt for a given agent role.
// ProjectID = nil means global default (applies to all projects).
type RolePrompt struct {
	ID        uuid.UUID  `json:"id"`
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
}
