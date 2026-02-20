package thread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateThread(ctx context.Context, t domainthread.Thread) (domainthread.Thread, error) {
	query := `
		INSERT INTO threads (id, project_id, task_id, type, name, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, project_id, task_id, type, name, created_at`

	var created domainthread.Thread
	err := r.pool.QueryRow(ctx, query,
		t.ID, t.ProjectID, t.TaskID, t.Type, t.Name, t.CreatedAt,
	).Scan(
		&created.ID, &created.ProjectID, &created.TaskID,
		&created.Type, &created.Name, &created.CreatedAt,
	)
	if err != nil {
		return domainthread.Thread{}, fmt.Errorf("inserting thread: %w", err)
	}
	return created, nil
}

func (r *Repository) GetThreadByID(ctx context.Context, id uuid.UUID) (domainthread.Thread, error) {
	query := `
		SELECT id, project_id, task_id, type, name, created_at
		FROM threads WHERE id = $1`

	var t domainthread.Thread
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.ProjectID, &t.TaskID, &t.Type, &t.Name, &t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domainthread.Thread{}, fmt.Errorf("thread %s not found", id)
		}
		return domainthread.Thread{}, fmt.Errorf("querying thread: %w", err)
	}
	return t, nil
}

func (r *Repository) ListThreads(ctx context.Context, filters domainthread.ListFilters) ([]domainthread.Thread, error) {
	query := `SELECT id, project_id, task_id, type, name, created_at FROM threads WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if filters.ProjectID != nil {
		query += fmt.Sprintf(" AND project_id = $%d", argIdx)
		args = append(args, *filters.ProjectID)
		argIdx++
	}
	if filters.TaskID != nil {
		query += fmt.Sprintf(" AND task_id = $%d", argIdx)
		args = append(args, *filters.TaskID)
		argIdx++
	}
	if filters.Type != nil {
		query += fmt.Sprintf(" AND type = $%d", argIdx)
		args = append(args, string(*filters.Type))
		argIdx++
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing threads: %w", err)
	}
	defer rows.Close()

	var threads []domainthread.Thread
	for rows.Next() {
		var t domainthread.Thread
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.TaskID, &t.Type, &t.Name, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning thread row: %w", err)
		}
		threads = append(threads, t)
	}
	return threads, rows.Err()
}

func (r *Repository) CreateMessage(ctx context.Context, m domainthread.Message) (domainthread.Message, error) {
	metadataJSON, err := json.Marshal(m.Metadata)
	if err != nil {
		return domainthread.Message{}, fmt.Errorf("marshaling metadata: %w", err)
	}

	query := `
		INSERT INTO messages (id, thread_id, agent_id, post_type, content, metadata_jsonb, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, thread_id, agent_id, post_type, content, metadata_jsonb, created_at`

	var created domainthread.Message
	var metaBytes []byte
	err = r.pool.QueryRow(ctx, query,
		m.ID, m.ThreadID, m.AgentID, m.PostType, m.Content, metadataJSON, m.CreatedAt,
	).Scan(
		&created.ID, &created.ThreadID, &created.AgentID,
		&created.PostType, &created.Content, &metaBytes, &created.CreatedAt,
	)
	if err != nil {
		return domainthread.Message{}, fmt.Errorf("inserting message: %w", err)
	}

	if len(metaBytes) > 0 {
		if err := json.Unmarshal(metaBytes, &created.Metadata); err != nil {
			return domainthread.Message{}, fmt.Errorf("unmarshaling metadata: %w", err)
		}
	}
	return created, nil
}

func (r *Repository) ListMessages(ctx context.Context, threadID uuid.UUID) ([]domainthread.Message, error) {
	query := `
		SELECT id, thread_id, agent_id, post_type, content, metadata_jsonb, created_at
		FROM messages WHERE thread_id = $1
		ORDER BY created_at ASC`

	rows, err := r.pool.Query(ctx, query, threadID)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	defer rows.Close()

	var messages []domainthread.Message
	for rows.Next() {
		var m domainthread.Message
		var metaBytes []byte
		if err := rows.Scan(
			&m.ID, &m.ThreadID, &m.AgentID, &m.PostType,
			&m.Content, &metaBytes, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}
		if len(metaBytes) > 0 {
			if err := json.Unmarshal(metaBytes, &m.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshaling metadata: %w", err)
			}
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (r *Repository) SetVisibility(ctx context.Context, v domainthread.Visibility) error {
	query := `
		INSERT INTO thread_visibility (thread_id, agent_role)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`

	_, err := r.pool.Exec(ctx, query, v.ThreadID, v.AgentRole)
	if err != nil {
		return fmt.Errorf("setting visibility: %w", err)
	}
	return nil
}

func (r *Repository) GetVisibleRoles(ctx context.Context, threadID uuid.UUID) ([]string, error) {
	query := `SELECT agent_role FROM thread_visibility WHERE thread_id = $1 ORDER BY agent_role`

	rows, err := r.pool.Query(ctx, query, threadID)
	if err != nil {
		return nil, fmt.Errorf("getting visible roles: %w", err)
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("scanning role: %w", err)
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *Repository) IsVisibleTo(ctx context.Context, threadID uuid.UUID, agentRole string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM thread_visibility WHERE thread_id = $1 AND agent_role = $2)`

	var exists bool
	err := r.pool.QueryRow(ctx, query, threadID, agentRole).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking visibility: %w", err)
	}
	return exists, nil
}
