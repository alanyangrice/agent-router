package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, t domaintask.Task) (domaintask.Task, error) {
	query := `
		INSERT INTO tasks (id, project_id, title, description, status, priority,
			assigned_agent_id, parent_task_id, branch_type, branch_name,
			labels, created_by, created_at, updated_at, started_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id, project_id, title, description, status, priority,
			assigned_agent_id, parent_task_id, branch_type, branch_name,
			labels, created_by, created_at, updated_at, started_at, completed_at`

	var created domaintask.Task
	err := r.pool.QueryRow(ctx, query,
		t.ID, t.ProjectID, t.Title, t.Description, t.Status, t.Priority,
		t.AssignedAgentID, t.ParentTaskID, t.BranchType, t.BranchName,
		t.Labels, t.CreatedBy, t.CreatedAt, t.UpdatedAt, t.StartedAt, t.CompletedAt,
	).Scan(
		&created.ID, &created.ProjectID, &created.Title, &created.Description,
		&created.Status, &created.Priority, &created.AssignedAgentID, &created.ParentTaskID,
		&created.BranchType, &created.BranchName, &created.Labels, &created.CreatedBy,
		&created.CreatedAt, &created.UpdatedAt, &created.StartedAt, &created.CompletedAt,
	)
	if err != nil {
		return domaintask.Task{}, fmt.Errorf("inserting task: %w", err)
	}
	return created, nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (domaintask.Task, error) {
	query := `
		SELECT id, project_id, title, description, status, priority,
			assigned_agent_id, parent_task_id, branch_type, branch_name,
			labels, created_by, created_at, updated_at, started_at, completed_at
		FROM tasks WHERE id = $1`

	var t domaintask.Task
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&t.AssignedAgentID, &t.ParentTaskID, &t.BranchType, &t.BranchName,
		&t.Labels, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domaintask.Task{}, fmt.Errorf("task %s not found", id)
		}
		return domaintask.Task{}, fmt.Errorf("querying task: %w", err)
	}
	return t, nil
}

func (r *Repository) List(ctx context.Context, filters domaintask.ListFilters) ([]domaintask.Task, error) {
	query := `
		SELECT id, project_id, title, description, status, priority,
			assigned_agent_id, parent_task_id, branch_type, branch_name,
			labels, created_by, created_at, updated_at, started_at, completed_at
		FROM tasks WHERE 1=1`

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

	query += " ORDER BY created_at DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

func (r *Repository) Update(ctx context.Context, t domaintask.Task) error {
	query := `
		UPDATE tasks SET
			title = $2, description = $3, status = $4, priority = $5,
			assigned_agent_id = $6, parent_task_id = $7, branch_type = $8,
			branch_name = $9, labels = $10, updated_at = $11,
			started_at = $12, completed_at = $13
		WHERE id = $1`

	tag, err := r.pool.Exec(ctx, query,
		t.ID, t.Title, t.Description, t.Status, t.Priority,
		t.AssignedAgentID, t.ParentTaskID, t.BranchType,
		t.BranchName, t.Labels, t.UpdatedAt,
		t.StartedAt, t.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("updating task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found", t.ID)
	}
	return nil
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, from, to domaintask.Status) error {
	var query string
	switch to {
	case domaintask.StatusInProgress:
		query = `UPDATE tasks SET status = $1, updated_at = NOW(), started_at = NOW() WHERE id = $2 AND status = $3`
	case domaintask.StatusDone:
		query = `UPDATE tasks SET status = $1, updated_at = NOW(), completed_at = NOW() WHERE id = $2 AND status = $3`
	default:
		query = `UPDATE tasks SET status = $1, updated_at = NOW() WHERE id = $2 AND status = $3`
	}

	tag, err := r.pool.Exec(ctx, query, string(to), id, string(from))
	if err != nil {
		return fmt.Errorf("updating task status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s status CAS failed: expected status %s", id, from)
	}
	return nil
}

func (r *Repository) Assign(ctx context.Context, taskID, agentID uuid.UUID) error {
	query := `UPDATE tasks SET assigned_agent_id = $1, updated_at = NOW() WHERE id = $2`

	tag, err := r.pool.Exec(ctx, query, agentID, taskID)
	if err != nil {
		return fmt.Errorf("assigning task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}
	return nil
}

func (r *Repository) Unassign(ctx context.Context, taskID uuid.UUID) error {
	query := `UPDATE tasks SET assigned_agent_id = NULL, updated_at = NOW() WHERE id = $1`

	tag, err := r.pool.Exec(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("unassigning task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}
	return nil
}

func (r *Repository) UnassignByAgent(ctx context.Context, agentID uuid.UUID) error {
	query := `
		UPDATE tasks
		SET assigned_agent_id = NULL, updated_at = NOW()
		WHERE assigned_agent_id = $1 AND status = 'ready'`

	_, err := r.pool.Exec(ctx, query, agentID)
	if err != nil {
		return fmt.Errorf("unassigning ready tasks for agent %s: %w", agentID, err)
	}
	return nil
}

func (r *Repository) AddDependency(ctx context.Context, dep domaintask.Dependency) error {
	query := `INSERT INTO task_dependencies (task_id, depends_on_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`

	_, err := r.pool.Exec(ctx, query, dep.TaskID, dep.DependsOnID)
	if err != nil {
		return fmt.Errorf("adding dependency: %w", err)
	}
	return nil
}

func (r *Repository) RemoveDependency(ctx context.Context, taskID, dependsOnID uuid.UUID) error {
	query := `DELETE FROM task_dependencies WHERE task_id = $1 AND depends_on_id = $2`

	_, err := r.pool.Exec(ctx, query, taskID, dependsOnID)
	if err != nil {
		return fmt.Errorf("removing dependency: %w", err)
	}
	return nil
}

func (r *Repository) GetDependencies(ctx context.Context, taskID uuid.UUID) ([]domaintask.Task, error) {
	query := `
		SELECT t.id, t.project_id, t.title, t.description, t.status, t.priority,
			t.assigned_agent_id, t.parent_task_id, t.branch_type, t.branch_name,
			t.labels, t.created_by, t.created_at, t.updated_at, t.started_at, t.completed_at
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

func (r *Repository) GetReadyTasks(ctx context.Context, projectID uuid.UUID, skills []string) ([]domaintask.Task, error) {
	// Ready tasks whose blocking dependencies are all done.
	query := `
		SELECT t.id, t.project_id, t.title, t.description, t.status, t.priority,
			t.assigned_agent_id, t.parent_task_id, t.branch_type, t.branch_name,
			t.labels, t.created_by, t.created_at, t.updated_at, t.started_at, t.completed_at
		FROM tasks t
		WHERE t.project_id = $1
		  AND t.status = 'ready'
		  AND t.assigned_agent_id IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM task_dependencies td
			JOIN tasks dep ON dep.id = td.depends_on_id
			WHERE td.task_id = t.id AND dep.status != 'done'
		  )`

	args := []interface{}{projectID}
	if len(skills) > 0 {
		query += " AND t.labels && $2"
		args = append(args, skills)
	}

	query += " ORDER BY CASE t.priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 END, t.created_at"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("getting ready tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

func scanTasks(rows pgx.Rows) ([]domaintask.Task, error) {
	var tasks []domaintask.Task
	for rows.Next() {
		var t domaintask.Task
		if err := rows.Scan(
			&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.Status, &t.Priority,
			&t.AssignedAgentID, &t.ParentTaskID, &t.BranchType, &t.BranchName,
			&t.Labels, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task rows: %w", err)
	}
	return tasks, nil
}
