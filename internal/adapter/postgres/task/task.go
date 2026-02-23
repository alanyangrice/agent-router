package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
)

// Repository implements port/task.Repository.
type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const taskSelectCols = `id, project_id, title, description, status, priority,
	assigned_agent_id, coder_id, parent_task_id, branch_type, branch_name,
	labels, COALESCE(required_role,''), COALESCE(pr_url,''), created_by, created_at, updated_at, started_at, completed_at`

func (r *Repository) Create(ctx context.Context, t domaintask.Task) (domaintask.Task, error) {
	query := `
		INSERT INTO tasks (id, project_id, title, description, status, priority,
			assigned_agent_id, coder_id, parent_task_id, branch_type, branch_name,
			labels, required_role, pr_url, created_by, created_at, updated_at, started_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING ` + taskSelectCols

	var created domaintask.Task
	err := r.pool.QueryRow(ctx, query,
		t.ID, t.ProjectID, t.Title, t.Description, t.Status, t.Priority,
		t.AssignedAgentID, t.CoderID, t.ParentTaskID, t.BranchType, t.BranchName,
		t.Labels, nilIfEmpty(t.RequiredRole), nilIfEmpty(t.PRUrl), t.CreatedBy,
		t.CreatedAt, t.UpdatedAt, t.StartedAt, t.CompletedAt,
	).Scan(scanTaskFields(&created)...)
	if err != nil {
		return domaintask.Task{}, fmt.Errorf("inserting task: %w", err)
	}
	return created, nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (domaintask.Task, error) {
	query := `SELECT ` + taskSelectCols + ` FROM tasks WHERE id = $1`

	var t domaintask.Task
	err := r.pool.QueryRow(ctx, query, id).Scan(scanTaskFields(&t)...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domaintask.Task{}, fmt.Errorf("task %s not found", id)
		}
		return domaintask.Task{}, fmt.Errorf("querying task: %w", err)
	}
	return t, nil
}

func (r *Repository) List(ctx context.Context, filters domaintask.ListFilters) ([]domaintask.Task, error) {
	query := `SELECT ` + taskSelectCols + ` FROM tasks WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filters.ProjectID != nil {
		query += fmt.Sprintf(" AND project_id = $%d", argIdx)
		args = append(args, *filters.ProjectID)
		argIdx++
	}
	if filters.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, string(*filters.Status))
		argIdx++
	}
	if filters.Priority != nil {
		query += fmt.Sprintf(" AND priority = $%d", argIdx)
		args = append(args, string(*filters.Priority))
		argIdx++
	}
	if filters.AssignedTo != nil {
		query += fmt.Sprintf(" AND assigned_agent_id = $%d", argIdx)
		args = append(args, *filters.AssignedTo)
		argIdx++
	}
	if len(filters.Labels) > 0 {
		query += fmt.Sprintf(" AND labels @> $%d", argIdx)
		args = append(args, filters.Labels)
		argIdx++
	}
	if filters.Unassigned {
		query += " AND assigned_agent_id IS NULL"
	}

	if filters.OldestFirst {
		query += " ORDER BY created_at ASC"
	} else {
		query += " ORDER BY created_at DESC"
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, from, to domaintask.Status) error {
	now := time.Now().UTC()
	var query string
	var args []interface{}

	switch to {
	case domaintask.StatusInProgress:
		// Set coder_id on first in_progress entry using COALESCE (preserves existing value on bounce-back).
		query = `UPDATE tasks SET status = $1, updated_at = $2, started_at = $2,
			coder_id = COALESCE(coder_id, assigned_agent_id)
			WHERE id = $3 AND status = $4`
		args = []interface{}{string(to), now, id, string(from)}
	case domaintask.StatusMerged:
		query = `UPDATE tasks SET status = $1, updated_at = $2, completed_at = $2 WHERE id = $3 AND status = $4`
		args = []interface{}{string(to), now, id, string(from)}
	default:
		query = `UPDATE tasks SET status = $1, updated_at = $2 WHERE id = $3 AND status = $4`
		args = []interface{}{string(to), now, id, string(from)}
	}

	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating task status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s status CAS failed: expected status %s", id, from)
	}
	return nil
}

func (r *Repository) SetPRUrl(ctx context.Context, id uuid.UUID, prURL string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE tasks SET pr_url = $1, updated_at = NOW() WHERE id = $2`, prURL, id)
	if err != nil {
		return fmt.Errorf("setting pr_url: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// Assign sets assigned_agent_id on a task, guarding against terminal statuses.
func (r *Repository) Assign(ctx context.Context, taskID, agentID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tasks SET assigned_agent_id = $1, updated_at = NOW()
		 WHERE id = $2 AND status NOT IN ('merged', 'backlog')`,
		agentID, taskID)
	if err != nil {
		return fmt.Errorf("assigning task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish terminal task from genuinely missing task.
		var count int
		_ = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE id=$1`, taskID).Scan(&count)
		if count == 0 {
			return fmt.Errorf("task not found: %s", taskID)
		}
		return fmt.Errorf("task in terminal status: %s", taskID)
	}
	return nil
}

// AssignIfIdle atomically locks the agent row and assigns the task only if the agent
// is still idle. Uses a data-modifying CTE so two concurrent calls for different tasks
// on the same agent are serialised at the row lock level — only one wins.
func (r *Repository) AssignIfIdle(ctx context.Context, taskID, agentID uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		WITH claimed AS (
		    UPDATE agents
		    SET    status = 'working', current_task_id = NULL
		    WHERE  id = $1 AND status = 'idle'
		    RETURNING id
		)
		UPDATE tasks SET assigned_agent_id = $1, updated_at = NOW()
		WHERE  id = $2
		AND    status NOT IN ('merged', 'backlog')
		AND    EXISTS (SELECT 1 FROM claimed)`,
		agentID, taskID)
	if err != nil {
		return false, fmt.Errorf("assign if idle: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) Unassign(ctx context.Context, taskID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE tasks SET assigned_agent_id = NULL, updated_at = NOW() WHERE id = $1`, taskID)
	if err != nil {
		return fmt.Errorf("unassigning task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}
	return nil
}

// UnassignByAgent releases only ready tasks — in-flight tasks are handled by the reaper.
func (r *Repository) UnassignByAgent(ctx context.Context, agentID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tasks SET assigned_agent_id = NULL, updated_at = NOW()
		WHERE assigned_agent_id = $1 AND status = 'ready'`, agentID)
	if err != nil {
		return fmt.Errorf("unassigning ready tasks for agent %s: %w", agentID, err)
	}
	return nil
}

// ReleaseInFlightByAgent resets in_progress tasks to ready and unassigns in_qa/in_review
// tasks, all in a single transaction. Returns the distinct statuses freed so the caller
// can sweep the right roles via pipeline.DefaultConfig.
func (r *Repository) ReleaseInFlightByAgent(ctx context.Context, agentID uuid.UUID) ([]domaintask.Status, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin release transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var freed []domaintask.Status

	tag, err := tx.Exec(ctx, `
		UPDATE tasks SET status='ready', assigned_agent_id=NULL, updated_at=NOW()
		WHERE assigned_agent_id=$1 AND status='in_progress'`, agentID)
	if err != nil {
		return nil, fmt.Errorf("release in_progress tasks: %w", err)
	}
	if tag.RowsAffected() > 0 {
		freed = append(freed, domaintask.StatusInProgress)
	}

	tag, err = tx.Exec(ctx, `
		UPDATE tasks SET assigned_agent_id=NULL, updated_at=NOW()
		WHERE assigned_agent_id=$1 AND status='in_qa'`, agentID)
	if err != nil {
		return nil, fmt.Errorf("release in_qa tasks: %w", err)
	}
	if tag.RowsAffected() > 0 {
		freed = append(freed, domaintask.StatusInQA)
	}

	tag, err = tx.Exec(ctx, `
		UPDATE tasks SET assigned_agent_id=NULL, updated_at=NOW()
		WHERE assigned_agent_id=$1 AND status='in_review'`, agentID)
	if err != nil {
		return nil, fmt.Errorf("release in_review tasks: %w", err)
	}
	if tag.RowsAffected() > 0 {
		freed = append(freed, domaintask.StatusInReview)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit release transaction: %w", err)
	}
	return freed, nil
}

func (r *Repository) AddDependency(ctx context.Context, dep domaintask.Dependency) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO task_dependencies (task_id, depends_on_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, dep.TaskID, dep.DependsOnID)
	if err != nil {
		return fmt.Errorf("adding dependency: %w", err)
	}
	return nil
}

func (r *Repository) RemoveDependency(ctx context.Context, taskID, dependsOnID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM task_dependencies WHERE task_id = $1 AND depends_on_id = $2`, taskID, dependsOnID)
	if err != nil {
		return fmt.Errorf("removing dependency: %w", err)
	}
	return nil
}

func (r *Repository) GetDependencies(ctx context.Context, taskID uuid.UUID) ([]domaintask.Task, error) {
	query := `
		SELECT ` + taskSelectCols + `
		FROM tasks t
		JOIN task_dependencies td ON td.depends_on_id = t.id
		WHERE td.task_id = $1
		ORDER BY t.created_at`

	rows, err := r.pool.Query(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("getting dependencies: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

// scanTaskFields returns the scan destination slice matching taskSelectCols order.
func scanTaskFields(t *domaintask.Task) []interface{} {
	return []interface{}{
		&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&t.AssignedAgentID, &t.CoderID, &t.ParentTaskID, &t.BranchType, &t.BranchName,
		&t.Labels, &t.RequiredRole, &t.PRUrl, &t.CreatedBy,
		&t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.CompletedAt,
	}
}

func scanTasks(rows pgx.Rows) ([]domaintask.Task, error) {
	var tasks []domaintask.Task
	for rows.Next() {
		var t domaintask.Task
		if err := rows.Scan(scanTaskFields(&t)...); err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task rows: %w", err)
	}
	return tasks, nil
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
