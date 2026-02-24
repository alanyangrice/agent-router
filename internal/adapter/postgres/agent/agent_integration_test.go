//go:build integration

package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgagent "github.com/alanyang/agent-mesh/internal/adapter/postgres/agent"
	pgproject "github.com/alanyang/agent-mesh/internal/adapter/postgres/project"
	pgtask "github.com/alanyang/agent-mesh/internal/adapter/postgres/task"
	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	"github.com/alanyang/agent-mesh/internal/service/distributor"
	"github.com/alanyang/agent-mesh/internal/testutil"
)

// helpers

func setupProject(t *testing.T, ctx context.Context, projRepo *pgproject.Repository) domainproject.Project {
	t.Helper()
	p := domainproject.Project{ID: uuid.New(), Name: "agent-test-" + uuid.New().String()[:8], RepoURL: "https://github.com/x", Config: map[string]interface{}{}}
	created, err := projRepo.Create(ctx, p)
	require.NoError(t, err)
	return created
}

func setupAgent(t *testing.T, ctx context.Context, repo *pgagent.Repository, projectID uuid.UUID, role string, status domainagent.Status) domainagent.Agent {
	t.Helper()
	a := domainagent.New(projectID, role, "bot-"+uuid.New().String()[:4], "gpt4", []string{})
	created, err := repo.Create(ctx, a)
	require.NoError(t, err)
	if status != domainagent.StatusIdle {
		require.NoError(t, repo.UpdateStatus(ctx, created.ID, status))
		created.Status = status
	}
	return created
}

func setupTask(t *testing.T, ctx context.Context, taskRepo *pgtask.Repository, projectID uuid.UUID, status domaintask.Status) domaintask.Task {
	t.Helper()
	task := domaintask.New(projectID, "task-"+uuid.New().String()[:8], "desc", domaintask.PriorityMedium, domaintask.BranchFix, "test")
	task.BranchName = "fix/" + task.ID.String()[:8]
	created, err := taskRepo.Create(ctx, task)
	require.NoError(t, err)
	if status != domaintask.StatusBacklog {
		from := domaintask.StatusBacklog
		to := domaintask.StatusReady
		require.NoError(t, taskRepo.UpdateStatus(ctx, created.ID, from, to))
		if status == domaintask.StatusInProgress {
			require.NoError(t, taskRepo.UpdateStatus(ctx, created.ID, domaintask.StatusReady, domaintask.StatusInProgress))
		} else if status == domaintask.StatusInQA {
			require.NoError(t, taskRepo.UpdateStatus(ctx, created.ID, domaintask.StatusReady, domaintask.StatusInProgress))
			require.NoError(t, taskRepo.UpdateStatus(ctx, created.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
		} else if status == domaintask.StatusInReview {
			require.NoError(t, taskRepo.UpdateStatus(ctx, created.ID, domaintask.StatusReady, domaintask.StatusInProgress))
			require.NoError(t, taskRepo.UpdateStatus(ctx, created.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
			require.NoError(t, taskRepo.UpdateStatus(ctx, created.ID, domaintask.StatusInQA, domaintask.StatusInReview))
		} else if status == domaintask.StatusReady {
			// already done
		}
		created.Status = status
	}
	return created
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestAgentRepo_CreateGetList(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	repo := pgagent.New(pool)
	proj := setupProject(t, ctx, projRepo)

	agent := setupAgent(t, ctx, repo, proj.ID, "coder", domainagent.StatusIdle)

	got, err := repo.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, got.ID)
	assert.Equal(t, "coder", got.Role)
	assert.Equal(t, domainagent.StatusIdle, got.Status)

	list, err := repo.List(ctx, domainagent.ListFilters{ProjectID: &proj.ID})
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestAgentRepo_UpdateStatus(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	repo := pgagent.New(pool)
	proj := setupProject(t, ctx, projRepo)

	agent := setupAgent(t, ctx, repo, proj.ID, "coder", domainagent.StatusIdle)
	require.NoError(t, repo.UpdateStatus(ctx, agent.ID, domainagent.StatusOffline))

	got, err := repo.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, domainagent.StatusOffline, got.Status)
}

func TestAgentRepo_SetWorkingStatus(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	proj := setupProject(t, ctx, projRepo)

	agent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	task := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusReady)

	require.NoError(t, agentRepo.SetWorkingStatus(ctx, agent.ID, task.ID))

	got, err := agentRepo.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, domainagent.StatusWorking, got.Status)
	require.NotNil(t, got.CurrentTaskID)
	assert.Equal(t, task.ID, *got.CurrentTaskID)
}

func TestAgentRepo_SetIdleStatus(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	proj := setupProject(t, ctx, projRepo)

	agent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	task := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusReady)
	require.NoError(t, agentRepo.SetWorkingStatus(ctx, agent.ID, task.ID))

	require.NoError(t, agentRepo.SetIdleStatus(ctx, agent.ID))

	got, err := agentRepo.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, domainagent.StatusIdle, got.Status)
	assert.Nil(t, got.CurrentTaskID)
}

func TestAgentRepo_ClaimAgent_ExcludesWorking(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	proj := setupProject(t, ctx, projRepo)

	idleAgent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	workingAgent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	task := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusReady)
	require.NoError(t, agentRepo.SetWorkingStatus(ctx, workingAgent.ID, task.ID))

	claimedID, err := agentRepo.ClaimAgent(ctx, proj.ID, "coder")
	require.NoError(t, err)
	assert.Equal(t, idleAgent.ID, claimedID, "working agent must be excluded from ClaimAgent")
}

func TestAgentRepo_ClaimAgent_ExcludesOffline(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	proj := setupProject(t, ctx, projRepo)

	idleAgent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	_ = setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusOffline)

	claimedID, err := agentRepo.ClaimAgent(ctx, proj.ID, "coder")
	require.NoError(t, err)
	assert.Equal(t, idleAgent.ID, claimedID, "offline agent must be excluded")
}

func TestAgentRepo_ClaimAgent_ZeroAgents(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	proj := setupProject(t, ctx, projRepo)
	repo := pgagent.New(pool)

	_, err := repo.ClaimAgent(ctx, proj.ID, "coder")
	require.Error(t, err)
	assert.ErrorIs(t, err, distributor.ErrNoAgentAvailable)
}

func TestAgentRepo_AssignIfIdle_AgentIdle(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	proj := setupProject(t, ctx, projRepo)

	agent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	task := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusInQA)

	ok, err := taskRepo.AssignIfIdle(ctx, task.ID, agent.ID)
	require.NoError(t, err)
	assert.True(t, ok, "AssignIfIdle must return true for idle agent")

	gotAgent, err := agentRepo.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, domainagent.StatusWorking, gotAgent.Status)

	gotTask, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	require.NotNil(t, gotTask.AssignedAgentID)
	assert.Equal(t, agent.ID, *gotTask.AssignedAgentID)
}

func TestAgentRepo_AssignIfIdle_AgentWorking(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	proj := setupProject(t, ctx, projRepo)

	agent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	task1 := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusInQA)
	task2 := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusInQA)
	require.NoError(t, agentRepo.SetWorkingStatus(ctx, agent.ID, task1.ID))

	ok, err := taskRepo.AssignIfIdle(ctx, task2.ID, agent.ID)
	require.NoError(t, err)
	assert.False(t, ok, "AssignIfIdle must return false for working agent")
}

func TestAgentRepo_AssignIfIdle_AgentOffline(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	proj := setupProject(t, ctx, projRepo)

	agent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusOffline)
	task := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusInQA)

	ok, err := taskRepo.AssignIfIdle(ctx, task.ID, agent.ID)
	require.NoError(t, err)
	assert.False(t, ok, "AssignIfIdle must return false for offline agent")
}

func TestAgentRepo_ListOfflineWithInflightTasks(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	proj := setupProject(t, ctx, projRepo)

	offlineAgent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusOffline)
	idleAgent := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	inProgressTask := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusInProgress)
	readyTask := setupTask(t, ctx, taskRepo, proj.ID, domaintask.StatusReady)

	require.NoError(t, taskRepo.Assign(ctx, inProgressTask.ID, offlineAgent.ID))
	require.NoError(t, taskRepo.Assign(ctx, readyTask.ID, idleAgent.ID))

	ids, err := agentRepo.ListOfflineWithInflightTasks(ctx)
	require.NoError(t, err)
	assert.Contains(t, ids, offlineAgent.ID)
	assert.NotContains(t, ids, idleAgent.ID)
}

func TestAgentRepo_ListOfflineWithInflightTasks_Empty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	proj := setupProject(t, ctx, projRepo)
	agentRepo := pgagent.New(pool)

	_ = setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)

	ids, err := agentRepo.ListOfflineWithInflightTasks(ctx)
	require.NoError(t, err)
	// Must return an empty slice (not nil) — callers rely on len check.
	if ids == nil {
		ids = []uuid.UUID{}
	}
	assert.Empty(t, ids)
}

// FIFO: oldest idle agent is claimed first.
func TestAgentRepo_ClaimAgent_FIFO(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	proj := setupProject(t, ctx, projRepo)

	first := setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)
	time.Sleep(2 * time.Millisecond) // ensure different created_at
	_ = setupAgent(t, ctx, agentRepo, proj.ID, "coder", domainagent.StatusIdle)

	claimedID, err := agentRepo.ClaimAgent(ctx, proj.ID, "coder")
	require.NoError(t, err)
	assert.Equal(t, first.ID, claimedID, "oldest idle agent should be claimed first (FIFO)")
}
