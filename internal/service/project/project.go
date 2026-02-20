package project

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID        uuid.UUID              `json:"id"`
	Name      string                 `json:"name"`
	RepoURL   string                 `json:"repo_url"`
	Config    map[string]interface{} `json:"config,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type Repository interface {
	Create(ctx context.Context, p Project) (Project, error)
	GetByID(ctx context.Context, id uuid.UUID) (Project, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, name, repoURL string) (Project, error) {
	p := Project{
		ID:        uuid.New(),
		Name:      name,
		RepoURL:   repoURL,
		Config:    map[string]interface{}{},
		CreatedAt: time.Now().UTC(),
	}

	created, err := s.repo.Create(ctx, p)
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (Project, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}
