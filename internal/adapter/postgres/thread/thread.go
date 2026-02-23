package thread

import (
	"context"
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
		t.ID, t.ProjectID, t.TaskID, string(t.Type), t.Name, t.CreatedAt,
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
	query := `SELECT id, project_id, task_id, type, name, created_at FROM threads WHERE id = $1`

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

	_ = argIdx // no more filters; suppress unused warning
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
	query := `
		INSERT INTO messages (id, thread_id, agent_id, post_type, content, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, thread_id, agent_id, post_type, content, created_at`

	var created domainthread.Message
	err := r.pool.QueryRow(ctx, query,
		m.ID, m.ThreadID, m.AgentID, string(m.PostType), m.Content, m.CreatedAt,
	).Scan(
		&created.ID, &created.ThreadID, &created.AgentID,
		&created.PostType, &created.Content, &created.CreatedAt,
	)
	if err != nil {
		return domainthread.Message{}, fmt.Errorf("inserting message: %w", err)
	}
	return created, nil
}

func (r *Repository) ListMessages(ctx context.Context, threadID uuid.UUID) ([]domainthread.Message, error) {
	query := `
		SELECT id, thread_id, agent_id, post_type, content, created_at
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
		if err := rows.Scan(
			&m.ID, &m.ThreadID, &m.AgentID, &m.PostType,
			&m.Content, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
