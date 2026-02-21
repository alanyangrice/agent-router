package prompt

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainprompt "github.com/alanyang/agent-mesh/internal/domain/prompt"
	portprompt "github.com/alanyang/agent-mesh/internal/port/prompt"
)

// Service provides role prompt resolution with project-specific → global default fallback.
// [SRP] Prompt resolution only.
// [DIP] Depends on PromptRepository port, not on any concrete storage.
type Service struct {
	repo portprompt.PromptRepository
}

func NewService(repo portprompt.PromptRepository) *Service {
	return &Service{repo: repo}
}

// GetForRole returns the effective role prompt for the given project and role.
// Falls back to the global default if no project-specific prompt exists.
func (s *Service) GetForRole(ctx context.Context, projectID uuid.UUID, role string) (domainprompt.RolePrompt, error) {
	p, err := s.repo.GetForRole(ctx, &projectID, role)
	if err != nil {
		return domainprompt.RolePrompt{}, fmt.Errorf("get role prompt: %w", err)
	}
	return p, nil
}

// Set upserts a project-specific role prompt.
func (s *Service) Set(ctx context.Context, projectID uuid.UUID, role, content string) error {
	p := domainprompt.RolePrompt{
		ProjectID: &projectID,
		Role:      role,
		Content:   content,
	}
	if err := s.repo.Set(ctx, p); err != nil {
		return fmt.Errorf("set role prompt: %w", err)
	}
	return nil
}

// List returns all role prompts for the project, merged with global defaults.
func (s *Service) List(ctx context.Context, projectID uuid.UUID) ([]domainprompt.RolePrompt, error) {
	prompts, err := s.repo.List(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list role prompts: %w", err)
	}
	return prompts, nil
}
