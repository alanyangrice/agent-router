package prompt

import (
	"context"

	"github.com/google/uuid"

	domainprompt "github.com/alanyang/agent-mesh/internal/domain/prompt"
)

// PromptRepository is the storage abstraction for role prompts.
// [DIP] service/prompt depends on this interface, not on any concrete storage.
// [LSP] Postgres, file-based, and in-memory implementations are all valid substitutes.
type PromptRepository interface {
	// GetForRole returns the role prompt for the given role, scoped to the project.
	// If projectID is nil or no project-specific prompt exists, returns the global default.
	GetForRole(ctx context.Context, projectID *uuid.UUID, role string) (domainprompt.RolePrompt, error)

	// Set upserts a role prompt for the given project (nil = global default).
	Set(ctx context.Context, p domainprompt.RolePrompt) error

	// List returns all role prompts for the given project, merged with global defaults
	// for roles that don't have a project-specific override.
	List(ctx context.Context, projectID uuid.UUID) ([]domainprompt.RolePrompt, error)
}
