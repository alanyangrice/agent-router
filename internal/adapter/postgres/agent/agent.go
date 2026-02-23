package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, a domainagent.Agent) (domainagent.Agent, error) {
	configJSON, err := json.Marshal(a.Config)
	if err != nil {
		return domainagent.Agent{}, fmt.Errorf("marshaling config: %w", err)
	}
	statsJSON, err := json.Marshal(a.Stats)
	if err != nil {
		return domainagent.Agent{}, fmt.Errorf("marshaling stats: %w", err)
	}

	query := `
		INSERT INTO agents (id, project_id, role, name, skills, model, status,
			current_task_id, config_jsonb, stats_jsonb, last_heartbeat_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, project_id, role, name, skills, model, status,
			current_task_id, config_jsonb, stats_jsonb, last_heartbeat_at, created_at`

	var created domainagent.Agent
	var cfgBytes, stBytes []byte
	err = r.pool.QueryRow(ctx, query,
		a.ID, a.ProjectID, a.Role, a.Name, a.Skills, a.Model, a.Status,
		a.CurrentTaskID, configJSON, statsJSON, a.LastHeartbeatAt, a.CreatedAt,
	).Scan(
		&created.ID, &created.ProjectID, &created.Role, &created.Name,
		&created.Skills, &created.Model, &created.Status, &created.CurrentTaskID,
		&cfgBytes, &stBytes, &created.LastHeartbeatAt, &created.CreatedAt,
	)
	if err != nil {
		return domainagent.Agent{}, fmt.Errorf("inserting agent: %w", err)
	}

	if err := unmarshalJSONFields(cfgBytes, stBytes, &created); err != nil {
		return domainagent.Agent{}, err
	}
	return created, nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (domainagent.Agent, error) {
	query := `
		SELECT id, project_id, role, name, skills, model, status,
			current_task_id, config_jsonb, stats_jsonb, last_heartbeat_at, created_at
		FROM agents WHERE id = $1`

	return r.scanOne(ctx, query, id)
}

func (r *Repository) List(ctx context.Context, filters domainagent.ListFilters) ([]domainagent.Agent, error) {
	query := `
		SELECT id, project_id, role, name, skills, model, status,
			current_task_id, config_jsonb, stats_jsonb, last_heartbeat_at, created_at
		FROM agents WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filters.ProjectID != nil {
		query += fmt.Sprintf(" AND project_id = $%d", argIdx)
		args = append(args, *filters.ProjectID)
		argIdx++
	}
	if filters.Role != nil {
		query += fmt.Sprintf(" AND role = $%d", argIdx)
		args = append(args, *filters.Role)
		argIdx++
	}
	if filters.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, string(*filters.Status))
		argIdx++
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	defer rows.Close()

	return scanAgents(rows)
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status domainagent.Status) error {
	tag, err := r.pool.Exec(ctx, `UPDATE agents SET status = $1 WHERE id = $2`, string(status), id)
	if err != nil {
		return fmt.Errorf("updating agent status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found", id)
	}
	return nil
}

func (r *Repository) SetCurrentTask(ctx context.Context, agentID uuid.UUID, taskID *uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE agents SET current_task_id = $1 WHERE id = $2`, taskID, agentID)
	if err != nil {
		return fmt.Errorf("setting current task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found", agentID)
	}
	return nil
}

func (r *Repository) scanOne(ctx context.Context, query string, args ...interface{}) (domainagent.Agent, error) {
	var a domainagent.Agent
	var cfgBytes, stBytes []byte

	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&a.ID, &a.ProjectID, &a.Role, &a.Name, &a.Skills, &a.Model,
		&a.Status, &a.CurrentTaskID, &cfgBytes, &stBytes,
		&a.LastHeartbeatAt, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domainagent.Agent{}, fmt.Errorf("agent not found")
		}
		return domainagent.Agent{}, fmt.Errorf("querying agent: %w", err)
	}

	if err := unmarshalJSONFields(cfgBytes, stBytes, &a); err != nil {
		return domainagent.Agent{}, err
	}
	return a, nil
}

func scanAgents(rows pgx.Rows) ([]domainagent.Agent, error) {
	var agents []domainagent.Agent
	for rows.Next() {
		var a domainagent.Agent
		var cfgBytes, stBytes []byte
		if err := rows.Scan(
			&a.ID, &a.ProjectID, &a.Role, &a.Name, &a.Skills, &a.Model,
			&a.Status, &a.CurrentTaskID, &cfgBytes, &stBytes,
			&a.LastHeartbeatAt, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning agent row: %w", err)
		}
		if err := unmarshalJSONFields(cfgBytes, stBytes, &a); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func unmarshalJSONFields(cfgBytes, stBytes []byte, a *domainagent.Agent) error {
	if len(cfgBytes) > 0 {
		if err := json.Unmarshal(cfgBytes, &a.Config); err != nil {
			return fmt.Errorf("unmarshaling config: %w", err)
		}
	}
	if len(stBytes) > 0 {
		if err := json.Unmarshal(stBytes, &a.Stats); err != nil {
			return fmt.Errorf("unmarshaling stats: %w", err)
		}
	}
	return nil
}
