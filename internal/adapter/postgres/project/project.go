package project

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	projectsvc "github.com/alanyang/agent-mesh/internal/service/project"
)

var _ projectsvc.Repository = (*Repository)(nil)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, p projectsvc.Project) (projectsvc.Project, error) {
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return projectsvc.Project{}, fmt.Errorf("marshal config: %w", err)
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO projects (id, name, repo_url, config_json, created_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, repo_url, config_json, created_at`,
		p.ID, p.Name, p.RepoURL, configJSON, p.CreatedAt,
	)

	var out projectsvc.Project
	var cfgBytes []byte
	if err := row.Scan(&out.ID, &out.Name, &out.RepoURL, &cfgBytes, &out.CreatedAt); err != nil {
		return projectsvc.Project{}, fmt.Errorf("insert project: %w", err)
	}
	if err := json.Unmarshal(cfgBytes, &out.Config); err != nil {
		out.Config = map[string]interface{}{}
	}
	return out, nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (projectsvc.Project, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, repo_url, config_json, created_at FROM projects WHERE id = $1`, id,
	)

	var out projectsvc.Project
	var cfgBytes []byte
	if err := row.Scan(&out.ID, &out.Name, &out.RepoURL, &cfgBytes, &out.CreatedAt); err != nil {
		return projectsvc.Project{}, fmt.Errorf("get project: %w", err)
	}
	if err := json.Unmarshal(cfgBytes, &out.Config); err != nil {
		out.Config = map[string]interface{}{}
	}
	return out, nil
}
