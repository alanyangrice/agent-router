package prompt

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domainprompt "github.com/alanyang/agent-mesh/internal/domain/prompt"
)

// Repository implements port/prompt.PromptRepository using Postgres.
// [LSP] Any conforming PromptRepository (file-based, in-memory) can substitute.
type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetForRole returns the role prompt for the given role, scoped to the project.
// Lookup order: project-specific → global default (project_id IS NULL).
func (r *Repository) GetForRole(ctx context.Context, projectID *uuid.UUID, role string) (domainprompt.RolePrompt, error) {
	if projectID != nil {
		query := `SELECT id, project_id, role, content, created_at FROM role_prompts WHERE project_id = $1 AND role = $2`
		var p domainprompt.RolePrompt
		err := r.pool.QueryRow(ctx, query, *projectID, role).Scan(
			&p.ID, &p.ProjectID, &p.Role, &p.Content, &p.CreatedAt,
		)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domainprompt.RolePrompt{}, fmt.Errorf("querying project prompt: %w", err)
		}
	}

	// Fall back to global default
	query := `SELECT id, project_id, role, content, created_at FROM role_prompts WHERE project_id IS NULL AND role = $1`
	var p domainprompt.RolePrompt
	err := r.pool.QueryRow(ctx, query, role).Scan(
		&p.ID, &p.ProjectID, &p.Role, &p.Content, &p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domainprompt.RolePrompt{}, fmt.Errorf("no prompt found for role %s", role)
		}
		return domainprompt.RolePrompt{}, fmt.Errorf("querying global prompt: %w", err)
	}
	return p, nil
}

// Set upserts a role prompt. If p.ProjectID is nil, sets the global default.
func (r *Repository) Set(ctx context.Context, p domainprompt.RolePrompt) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}

	query := `
		INSERT INTO role_prompts (id, project_id, role, content)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, role) DO UPDATE SET content = EXCLUDED.content`

	_, err := r.pool.Exec(ctx, query, p.ID, p.ProjectID, p.Role, p.Content)
	if err != nil {
		return fmt.Errorf("upserting role prompt: %w", err)
	}
	return nil
}

// List returns all role prompts for the given project, with global defaults for
// roles that don't have a project-specific override.
func (r *Repository) List(ctx context.Context, projectID uuid.UUID) ([]domainprompt.RolePrompt, error) {
	query := `
		SELECT COALESCE(proj.id, glob.id),
		       proj.project_id,
		       COALESCE(proj.role, glob.role),
		       COALESCE(proj.content, glob.content),
		       COALESCE(proj.created_at, glob.created_at)
		FROM (SELECT * FROM role_prompts WHERE project_id IS NULL) glob
		LEFT JOIN (SELECT * FROM role_prompts WHERE project_id = $1) proj
			ON glob.role = proj.role
		ORDER BY glob.role`

	rows, err := r.pool.Query(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("listing role prompts: %w", err)
	}
	defer rows.Close()

	var prompts []domainprompt.RolePrompt
	for rows.Next() {
		var p domainprompt.RolePrompt
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Role, &p.Content, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning role prompt row: %w", err)
		}
		prompts = append(prompts, p)
	}
	return prompts, rows.Err()
}
