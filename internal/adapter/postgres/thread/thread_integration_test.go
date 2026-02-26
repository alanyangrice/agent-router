//go:build integration

package thread_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgproject "github.com/alanyang/agent-mesh/internal/adapter/postgres/project"
	pgtask "github.com/alanyang/agent-mesh/internal/adapter/postgres/task"
	pgthread "github.com/alanyang/agent-mesh/internal/adapter/postgres/thread"
	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	"github.com/alanyang/agent-mesh/internal/testutil"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func mkProject(t *testing.T, ctx context.Context, r *pgproject.Repository) domainproject.Project {
	t.Helper()
	p := domainproject.Project{
		ID:      uuid.New(),
		Name:    "th-" + uuid.New().String()[:6],
		RepoURL: "https://g",
		Config:  map[string]interface{}{},
	}
	c, err := r.Create(ctx, p)
	require.NoError(t, err)
	return c
}

func mkTask(t *testing.T, ctx context.Context, r *pgtask.Repository, projID uuid.UUID) domaintask.Task {
	t.Helper()
	task := domaintask.New(projID, "task-"+uuid.New().String()[:6], "", domaintask.PriorityLow, domaintask.BranchFix, "t")
	task.BranchName = "fix/" + task.ID.String()[:8]
	c, err := r.Create(ctx, task)
	require.NoError(t, err)
	return c
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestThreadRepo_CreateGetList(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	threadRepo := pgthread.New(pool)
	proj := mkProject(t, ctx, projRepo)

	thread := domainthread.New(proj.ID, domainthread.TypeTask, "my-thread", nil)
	created, err := threadRepo.CreateThread(ctx, thread)
	require.NoError(t, err)
	assert.Equal(t, thread.ID, created.ID)
	assert.Equal(t, domainthread.TypeTask, created.Type)

	got, err := threadRepo.GetThreadByID(ctx, thread.ID)
	require.NoError(t, err)
	assert.Equal(t, thread.ID, got.ID)

	list, err := threadRepo.ListThreads(ctx, domainthread.ListFilters{ProjectID: &proj.ID})
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestThreadRepo_PostMessage(t *testing.T) {
	t.Run("insertion order is stable", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		projRepo := pgproject.New(pool)
		threadRepo := pgthread.New(pool)
		proj := mkProject(t, ctx, projRepo)

		thread := domainthread.New(proj.ID, domainthread.TypeTask, "t", nil)
		_, err := threadRepo.CreateThread(ctx, thread)
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			msg := domainthread.NewMessage(thread.ID, nil, domainthread.PostProgress, "msg")
			_, err := threadRepo.CreateMessage(ctx, msg)
			require.NoError(t, err)
		}

		msgs, err := threadRepo.ListMessages(ctx, thread.ID)
		require.NoError(t, err)
		require.Len(t, msgs, 3)
		assert.True(t, !msgs[0].CreatedAt.After(msgs[1].CreatedAt), "messages must be in insertion order")
		assert.True(t, !msgs[1].CreatedAt.After(msgs[2].CreatedAt))
	})
}

func TestThreadRepo_ThreadStillReadableAfterMerge(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	taskRepo := pgtask.New(pool)
	threadRepo := pgthread.New(pool)
	proj := mkProject(t, ctx, projRepo)

	task := mkTask(t, ctx, taskRepo, proj.ID)
	thread := domainthread.New(proj.ID, domainthread.TypeTask, "t", &task.ID)
	_, err := threadRepo.CreateThread(ctx, thread)
	require.NoError(t, err)

	msg := domainthread.NewMessage(thread.ID, nil, domainthread.PostProgress, "progress update")
	_, err = threadRepo.CreateMessage(ctx, msg)
	require.NoError(t, err)

	// Advance task to merged.
	require.NoError(t, taskRepo.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, taskRepo.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	require.NoError(t, taskRepo.UpdateStatus(ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
	require.NoError(t, taskRepo.UpdateStatus(ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInReview))
	require.NoError(t, taskRepo.UpdateStatus(ctx, task.ID, domaintask.StatusInReview, domaintask.StatusMerged))

	// Messages must still be readable after merge.
	msgs, err := threadRepo.ListMessages(ctx, thread.ID)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "progress update", msgs[0].Content)
}

func TestThreadRepo_ListThreads(t *testing.T) {
	t.Run("task_id filter returns only matching thread", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		projRepo := pgproject.New(pool)
		taskRepo := pgtask.New(pool)
		threadRepo := pgthread.New(pool)
		proj := mkProject(t, ctx, projRepo)

		task1 := mkTask(t, ctx, taskRepo, proj.ID)
		task2 := mkTask(t, ctx, taskRepo, proj.ID)

		thread1 := domainthread.New(proj.ID, domainthread.TypeTask, "t1", &task1.ID)
		thread2 := domainthread.New(proj.ID, domainthread.TypeTask, "t2", &task2.ID)
		_, err := threadRepo.CreateThread(ctx, thread1)
		require.NoError(t, err)
		_, err = threadRepo.CreateThread(ctx, thread2)
		require.NoError(t, err)

		list, err := threadRepo.ListThreads(ctx, domainthread.ListFilters{TaskID: &task1.ID})
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, thread1.ID, list[0].ID)
	})
}
