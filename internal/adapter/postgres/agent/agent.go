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
	"github.com/alanyang/agent-mesh/internal/service/distributor"
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

// ClaimAgent atomically selects the oldest idle agent with the given role and marks it
// working, using FOR UPDATE SKIP LOCKED and a NOT EXISTS guard that skips agents who
// already have in-flight tasks (prevents double-assignment on reconnect).
func (r *Repository) ClaimAgent(ctx context.Context, projectID uuid.UUID, role string) (uuid.UUID, error) {
	var agentID uuid.UUID
	err := r.pool.QueryRow(ctx, `
		UPDATE agents
		SET    status = 'working', current_task_id = NULL
		WHERE  id = (
		    SELECT a.id FROM agents a
		    WHERE  a.project_id = $1
		    AND    a.role       = $2
		    AND    a.status     = 'idle'
		    AND NOT EXISTS (
		        SELECT 1 FROM tasks t
		        WHERE  t.assigned_agent_id = a.id
		        AND    t.status NOT IN ('merged', 'backlog')
		    )
		    ORDER  BY a.created_at ASC
		    LIMIT  1
		    FOR UPDATE SKIP LOCKED
		)
		RETURNING id`, projectID, role).Scan(&agentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, distributor.ErrNoAgentAvailable
		}
		return uuid.Nil, fmt.Errorf("claim agent: %w", err)
	}
	return agentID, nil
}

// SetIdleStatus marks an agent idle with no current task.
func (r *Repository) SetIdleStatus(ctx context.Context, agentID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE agents SET status = 'idle', current_task_id = NULL WHERE id = $1`,
		agentID)
	if err != nil {
		return fmt.Errorf("set idle status: %w", err)
	}
	return nil
}

// SetWorkingStatus marks an agent as working on a specific task, recording current_task_id.
func (r *Repository) SetWorkingStatus(ctx context.Context, agentID uuid.UUID, taskID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE agents SET status = 'working', current_task_id = $2 WHERE id = $1`,
		agentID, taskID)
	if err != nil {
		return fmt.Errorf("set working status: %w", err)
	}
	return nil
}

// ListOfflineWithInflightTasks returns IDs of offline agents that still have in_progress,
// in_qa, or in_review tasks assigned to them. Used on startup to reschedule reaper timers.
func (r *Repository) ListOfflineWithInflightTasks(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT a.id FROM agents a
		JOIN tasks t ON t.assigned_agent_id = a.id
		WHERE a.status = 'offline'
		  AND t.status IN ('in_progress', 'in_qa', 'in_review')`)
	if err != nil {
		return nil, fmt.Errorf("listing offline agents with inflight tasks: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning agent id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
