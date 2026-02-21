package project

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	portproject "github.com/alanyang/agent-mesh/internal/port/project"
)

type Service struct {
	repo portproject.Repository
}

func NewService(repo portproject.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, name, repoURL string) (domainproject.Project, error) {
	p := domainproject.Project{
		ID:        uuid.New(),
		Name:      name,
		RepoURL:   repoURL,
		Config:    map[string]interface{}{},
		CreatedAt: time.Now().UTC(),
	}

	created, err := s.repo.Create(ctx, p)
	if err != nil {
		return domainproject.Project{}, fmt.Errorf("create project: %w", err)
	}
	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (domainproject.Project, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domainproject.Project{}, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}
