//go:build integration

package task_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgagent "github.com/alanyang/agent-mesh/internal/adapter/postgres/agent"
	pgproject "github.com/alanyang/agent-mesh/internal/adapter/postgres/project"
	pgtask "github.com/alanyang/agent-mesh/internal/adapter/postgres/task"
	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	"github.com/alanyang/agent-mesh/internal/testutil"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func makeProject(t *testing.T, ctx context.Context, r *pgproject.Repository) domainproject.Project {
	t.Helper()
	p := domainproject.Project{
		ID:      uuid.New(),
		Name:    "t-" + uuid.New().String()[:6],
		RepoURL: "https://g.io",
		Config:  map[string]interface{}{},
	}
	created, err := r.Create(ctx, p)
	require.NoError(t, err)
	return created
}

func makeAgent(t *testing.T, ctx context.Context, r *pgagent.Repository, projID uuid.UUID) domainagent.Agent {
	t.Helper()
	a := domainagent.New(projID, "coder", "bot", "gpt4", []string{})
	created, err := r.Create(ctx, a)
	require.NoError(t, err)
	return created
}

func makeTask(t *testing.T, ctx context.Context, r *pgtask.Repository, projID uuid.UUID) domaintask.Task {
	t.Helper()
	task := domaintask.New(projID, "t-"+uuid.New().String()[:8], "", domaintask.PriorityLow, domaintask.BranchFix, "test")
	task.BranchName = "fix/" + task.ID.String()[:8]
	created, err := r.Create(ctx, task)
	require.NoError(t, err)
	return created
}

func advanceTask(t *testing.T, ctx context.Context, r *pgtask.Repository, taskID uuid.UUID, to domaintask.Status) {
	t.Helper()
	var transitions [][]domaintask.Status
	switch to {
	case domaintask.StatusReady:
		transitions = [][]domaintask.Status{{domaintask.StatusBacklog, domaintask.StatusReady}}
	case domaintask.StatusInProgress:
		transitions = [][]domaintask.Status{
			{domaintask.StatusBacklog, domaintask.StatusReady},
			{domaintask.StatusReady, domaintask.StatusInProgress},
		}
	case domaintask.StatusInQA:
		transitions = [][]domaintask.Status{
			{domaintask.StatusBacklog, domaintask.StatusReady},
			{domaintask.StatusReady, domaintask.StatusInProgress},
			{domaintask.StatusInProgress, domaintask.StatusInQA},
		}
	case domaintask.StatusInReview:
		transitions = [][]domaintask.Status{
			{domaintask.StatusBacklog, domaintask.StatusReady},
			{domaintask.StatusReady, domaintask.StatusInProgress},
			{domaintask.StatusInProgress, domaintask.StatusInQA},
			{domaintask.StatusInQA, domaintask.StatusInReview},
		}
	}
	for _, tr := range transitions {
		require.NoError(t, r.UpdateStatus(ctx, taskID, tr[0], tr[1]))
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestTaskRepo_CreateGetList(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	taskRepo := pgtask.New(pool)
	proj := makeProject(t, ctx, projRepo)

	task := makeTask(t, ctx, taskRepo, proj.ID)
	assert.Equal(t, domaintask.StatusBacklog, task.Status)

	got, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.ID, got.ID)

	list, err := taskRepo.List(ctx, domaintask.ListFilters{ProjectID: &proj.ID})
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestTaskRepo_UpdateStatus(t *testing.T) {
	t.Run("CAS succeeds on correct from-status", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		projRepo := pgproject.New(pool)
		taskRepo := pgtask.New(pool)
		proj := makeProject(t, ctx, projRepo)
		task := makeTask(t, ctx, taskRepo, proj.ID)

		err := taskRepo.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady)
		require.NoError(t, err)
	})

	t.Run("CAS fails when current status does not match from", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		projRepo := pgproject.New(pool)
		taskRepo := pgtask.New(pool)
		proj := makeProject(t, ctx, projRepo)
		task := makeTask(t, ctx, taskRepo, proj.ID)

		require.NoError(t, taskRepo.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
		// Now status is ready, so backlog→ready should fail.
		err := taskRepo.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady)
		require.Error(t, err, "CAS must fail when current status does not match from")
	})
}

func TestTaskRepo_Assign(t *testing.T) {
	t.Run("assign then unassign clears agent", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		projRepo := pgproject.New(pool)
		agentRepo := pgagent.New(pool)
		taskRepo := pgtask.New(pool)
		proj := makeProject(t, ctx, projRepo)
		agent := makeAgent(t, ctx, agentRepo, proj.ID)
		task := makeTask(t, ctx, taskRepo, proj.ID)
		advanceTask(t, ctx, taskRepo, task.ID, domaintask.StatusReady)

		require.NoError(t, taskRepo.Assign(ctx, task.ID, agent.ID))
		got, _ := taskRepo.GetByID(ctx, task.ID)
		require.NotNil(t, got.AssignedAgentID)
		assert.Equal(t, agent.ID, *got.AssignedAgentID)

		require.NoError(t, taskRepo.Unassign(ctx, task.ID))
		got, _ = taskRepo.GetByID(ctx, task.ID)
		assert.Nil(t, got.AssignedAgentID)
	})

	t.Run("UnassignByAgent only unassigns ready tasks (in_progress and in_qa preserved)", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		projRepo := pgproject.New(pool)
		agentRepo := pgagent.New(pool)
		taskRepo := pgtask.New(pool)
		proj := makeProject(t, ctx, projRepo)
		agent := makeAgent(t, ctx, agentRepo, proj.ID)

		readyTask := makeTask(t, ctx, taskRepo, proj.ID)
		advanceTask(t, ctx, taskRepo, readyTask.ID, domaintask.StatusReady)
		require.NoError(t, taskRepo.Assign(ctx, readyTask.ID, agent.ID))

		inProgressTask := makeTask(t, ctx, taskRepo, proj.ID)
		advanceTask(t, ctx, taskRepo, inProgressTask.ID, domaintask.StatusInProgress)
		require.NoError(t, taskRepo.Assign(ctx, inProgressTask.ID, agent.ID))

		inQATask := makeTask(t, ctx, taskRepo, proj.ID)
		advanceTask(t, ctx, taskRepo, inQATask.ID, domaintask.StatusInQA)
		require.NoError(t, taskRepo.Assign(ctx, inQATask.ID, agent.ID))

		require.NoError(t, taskRepo.UnassignByAgent(ctx, agent.ID))

		ready, _ := taskRepo.GetByID(ctx, readyTask.ID)
		assert.Nil(t, ready.AssignedAgentID, "ready task must be unassigned")

		inProgress, _ := taskRepo.GetByID(ctx, inProgressTask.ID)
		assert.Equal(t, agent.ID, *inProgress.AssignedAgentID, "in_progress must remain assigned (grace period)")

		inQA, _ := taskRepo.GetByID(ctx, inQATask.ID)
		assert.Equal(t, agent.ID, *inQA.AssignedAgentID, "in_qa must remain assigned (grace period)")
	})

	t.Run("ReleaseInFlightByAgent resets in_progress to ready, preserves in_qa and in_review status", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		projRepo := pgproject.New(pool)
		agentRepo := pgagent.New(pool)
		taskRepo := pgtask.New(pool)
		proj := makeProject(t, ctx, projRepo)
		agent := makeAgent(t, ctx, agentRepo, proj.ID)

		inProgressTask := makeTask(t, ctx, taskRepo, proj.ID)
		advanceTask(t, ctx, taskRepo, inProgressTask.ID, domaintask.StatusInProgress)
		require.NoError(t, taskRepo.Assign(ctx, inProgressTask.ID, agent.ID))

		inQATask := makeTask(t, ctx, taskRepo, proj.ID)
		advanceTask(t, ctx, taskRepo, inQATask.ID, domaintask.StatusInQA)
		require.NoError(t, taskRepo.Assign(ctx, inQATask.ID, agent.ID))

		inReviewTask := makeTask(t, ctx, taskRepo, proj.ID)
		advanceTask(t, ctx, taskRepo, inReviewTask.ID, domaintask.StatusInReview)
		require.NoError(t, taskRepo.Assign(ctx, inReviewTask.ID, agent.ID))

		freed, err := taskRepo.ReleaseInFlightByAgent(ctx, agent.ID)
		require.NoError(t, err)

		freedSet := make(map[domaintask.Status]bool)
		for _, s := range freed {
			freedSet[s] = true
		}
		assert.True(t, freedSet[domaintask.StatusInProgress])
		assert.True(t, freedSet[domaintask.StatusInQA])
		assert.True(t, freedSet[domaintask.StatusInReview])

		ip, _ := taskRepo.GetByID(ctx, inProgressTask.ID)
		assert.Equal(t, domaintask.StatusReady, ip.Status, "in_progress must be reset to ready")
		assert.Nil(t, ip.AssignedAgentID)

		qa, _ := taskRepo.GetByID(ctx, inQATask.ID)
		assert.Equal(t, domaintask.StatusInQA, qa.Status, "in_qa status preserved")
		assert.Nil(t, qa.AssignedAgentID)

		rev, _ := taskRepo.GetByID(ctx, inReviewTask.ID)
		assert.Equal(t, domaintask.StatusInReview, rev.Status, "in_review status preserved")
		assert.Nil(t, rev.AssignedAgentID)
	})
}

func TestTaskRepo_List_UnassignedFilter(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	proj := makeProject(t, ctx, projRepo)
	agent := makeAgent(t, ctx, agentRepo, proj.ID)

	assigned := makeTask(t, ctx, taskRepo, proj.ID)
	advanceTask(t, ctx, taskRepo, assigned.ID, domaintask.StatusReady)
	require.NoError(t, taskRepo.Assign(ctx, assigned.ID, agent.ID))

	unassigned := makeTask(t, ctx, taskRepo, proj.ID)
	advanceTask(t, ctx, taskRepo, unassigned.ID, domaintask.StatusReady)

	list, err := taskRepo.List(ctx, domaintask.ListFilters{ProjectID: &proj.ID, Unassigned: true})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, unassigned.ID, list[0].ID)
}

func TestTaskRepo_Dependencies(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	taskRepo := pgtask.New(pool)
	proj := makeProject(t, ctx, projRepo)

	task1 := makeTask(t, ctx, taskRepo, proj.ID)
	task2 := makeTask(t, ctx, taskRepo, proj.ID)

	dep := domaintask.Dependency{TaskID: task1.ID, DependsOnID: task2.ID}
	require.NoError(t, taskRepo.AddDependency(ctx, dep))

	deps, err := taskRepo.GetDependencies(ctx, task1.ID)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, task2.ID, deps[0].ID)

	require.NoError(t, taskRepo.RemoveDependency(ctx, task1.ID, task2.ID))
	deps, err = taskRepo.GetDependencies(ctx, task1.ID)
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestTaskRepo_SetPRUrl(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	projRepo := pgproject.New(pool)
	taskRepo := pgtask.New(pool)
	proj := makeProject(t, ctx, projRepo)
	task := makeTask(t, ctx, taskRepo, proj.ID)

	require.NoError(t, taskRepo.SetPRUrl(ctx, task.ID, "https://github.com/pr/42"))

	got, err := taskRepo.GetByID(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/pr/42", got.PRUrl)
}
