//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/jackc/pgx/v5/pgxpool"

	pgagent "github.com/alanyang/agent-mesh/internal/adapter/postgres/agent"
	pgeventbus "github.com/alanyang/agent-mesh/internal/adapter/postgres/eventbus"
	pglocker "github.com/alanyang/agent-mesh/internal/adapter/postgres/locker"
	pgproject "github.com/alanyang/agent-mesh/internal/adapter/postgres/project"
	pgtask "github.com/alanyang/agent-mesh/internal/adapter/postgres/task"
	pgthread "github.com/alanyang/agent-mesh/internal/adapter/postgres/thread"
	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	distsvc "github.com/alanyang/agent-mesh/internal/service/distributor"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
	"github.com/alanyang/agent-mesh/internal/testutil"
)

// ── test harness ──────────────────────────────────────────────────────────────

type testServices struct {
	pool      *pgxpool.Pool
	agentRepo *pgagent.Repository
	taskRepo  *pgtask.Repository
	agentSvc  *agentsvc.Service
	taskSvc   *tasksvc.Service
	threadSvc *threadsvc.Service
	notifier  *testutil.CaptureNotifier
	projectID uuid.UUID
}

func newTestServices(t *testing.T) *testServices {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	projRepo := pgproject.New(pool)
	agentRepo := pgagent.New(pool)
	taskRepo := pgtask.New(pool)
	threadRepo := pgthread.New(pool)
	bus := pgeventbus.New(pool)
	locker := pglocker.New(pool)
	notifier := &testutil.CaptureNotifier{}
	distSvc := distsvc.NewService(agentRepo)

	// Create isolated project for this test.
	proj := domainproject.Project{
		ID:      uuid.New(),
		Name:    "integration-" + uuid.New().String()[:8],
		RepoURL: "https://github.com/test",
		Config:  map[string]interface{}{},
	}
	_, err := projRepo.Create(ctx, proj)
	require.NoError(t, err)

	agSvc := agentsvc.NewService(agentRepo, taskRepo, bus)
	tSvc := tasksvc.NewService(taskRepo, bus, distSvc, threadRepo, notifier, notifier, pipeline.DefaultConfig, locker)
	thSvc := threadsvc.NewService(threadRepo, bus)

	return &testServices{
		pool:      pool,
		agentRepo: agentRepo,
		taskRepo:  taskRepo,
		agentSvc:  agSvc,
		taskSvc:   tSvc,
		threadSvc: thSvc,
		notifier:  notifier,
		projectID: proj.ID,
	}
}

// registerAgent simulates the MCP register_agent call + explicit sweep.
func (s *testServices) registerAgent(t *testing.T, ctx context.Context, role string) uuid.UUID {
	t.Helper()
	agent, err := s.agentSvc.Register(ctx, s.projectID, role, "bot", "gpt4", []string{})
	require.NoError(t, err)
	// Explicit sweep (MCP fires this in a goroutine; here we call directly for determinism).
	require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, role))
	return agent.ID
}

// claimTask simulates the MCP claim_task call.
// Returns the first non-terminal task assigned to agentID, or nil if none.
func (s *testServices) claimTask(t *testing.T, ctx context.Context, agentID uuid.UUID) *domaintask.Task {
	t.Helper()
	tasks, err := s.taskSvc.List(ctx, domaintask.ListFilters{AssignedTo: &agentID, OldestFirst: true})
	require.NoError(t, err)
	for i := range tasks {
		if tasks[i].Status != domaintask.StatusMerged && tasks[i].Status != domaintask.StatusBacklog {
			s.agentSvc.SetWorking(ctx, agentID, tasks[i].ID)
			return &tasks[i]
		}
	}
	s.agentSvc.SetIdle(ctx, agentID)
	require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, s.agentRole(t, ctx, agentID)))
	return nil
}

func (s *testServices) agentRole(t *testing.T, ctx context.Context, agentID uuid.UUID) string {
	t.Helper()
	a, err := s.agentSvc.GetByID(ctx, agentID)
	require.NoError(t, err)
	return a.Role
}

// updateStatus simulates update_task_status + explicit freed-role sweep.
func (s *testServices) updateStatus(t *testing.T, ctx context.Context, taskID uuid.UUID, from, to domaintask.Status) {
	t.Helper()
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, taskID, from, to))
	// Derive freed role from pipeline config and sweep explicitly.
	if action, ok := pipeline.DefaultConfig[from]; ok {
		if role := action.EffectiveFreedRole(); role != "" {
			require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, role))
		}
	}
}

func (s *testServices) createTask(t *testing.T, ctx context.Context) domaintask.Task {
	t.Helper()
	task, err := s.taskSvc.Create(ctx, s.projectID, "task-"+uuid.New().String()[:8], "desc", domaintask.PriorityMedium, domaintask.BranchFix, "test")
	require.NoError(t, err)
	return task
}

func (s *testServices) getTask(t *testing.T, ctx context.Context, taskID uuid.UUID) domaintask.Task {
	t.Helper()
	task, err := s.taskSvc.GetByID(ctx, taskID)
	require.NoError(t, err)
	return task
}

// ── Scenario 1: Task first distributed ───────────────────────────────────────

func TestScenario1_TaskDistributed_AgentIdle(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)

	s.updateStatus(t, ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady)

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusReady, got.Status)
	require.NotNil(t, got.AssignedAgentID, "task must be assigned to the idle coder")
	assert.Equal(t, coderID, *got.AssignedAgentID)
	assert.NotEmpty(t, s.notifier.AgentNotifications(coderID), "coder must receive task_assigned notification")
}

func TestScenario1_TaskDistributed_NoAgent(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)
	// No agents registered.
	task := s.createTask(t, ctx)

	s.updateStatus(t, ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady)

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusReady, got.Status)
	assert.Nil(t, got.AssignedAgentID, "task must stay unassigned when no agents available")
}

func TestScenario1_SweepOnCoderRegister(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)
	task := s.createTask(t, ctx)

	// Advance to ready with no coders → task stays unassigned.
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	got := s.getTask(t, ctx, task.ID)
	assert.Nil(t, got.AssignedAgentID)

	// Coder registers → sweep fires → task gets assigned.
	coderID := s.registerAgent(t, ctx, "coder")

	got = s.getTask(t, ctx, task.ID)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, coderID, *got.AssignedAgentID)
}

// ── Scenario 2: Coder submits to QA ──────────────────────────────────────────

func TestScenario2_CoderSubmitsToQA_QAIdle(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	qaID := s.registerAgent(t, ctx, "qa")
	task := s.createTask(t, ctx)

	// Assign task to coder and advance to in_progress.
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	_ = qaID

	s.notifier.Reset()
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusInQA, got.Status)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, qaID, *got.AssignedAgentID)
	assert.NotEmpty(t, s.notifier.AgentNotifications(qaID))
}

func TestScenario2_CoderSubmitsToQA_NoQA(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)

	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))

	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusInQA, got.Status)
	assert.Nil(t, got.AssignedAgentID, "task must be unassigned when no QA available")
}

func TestScenario2_SweepTriggeredForCoderRole(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	task1 := s.createTask(t, ctx)
	task2 := s.createTask(t, ctx) // second ready task waiting

	// Set up task1 as in_progress for coder.
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusReady, domaintask.StatusInProgress))

	// Advance task2 to ready without assigning.
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusBacklog, domaintask.StatusReady))

	s.notifier.Reset()
	// Coder submits task1 to QA → frees coder role → sweep fires for "coder".
	s.updateStatus(t, ctx, task1.ID, domaintask.StatusInProgress, domaintask.StatusInQA)

	got2 := s.getTask(t, ctx, task2.ID)
	require.NotNil(t, got2.AssignedAgentID, "sweep must assign waiting ready task to freed coder")
	assert.Equal(t, coderID, *got2.AssignedAgentID)
}

// ── Scenario 3: QA claims task ────────────────────────────────────────────────

func TestScenario3_QA_ClaimTask_AfterAssignment(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	qaID := s.registerAgent(t, ctx, "qa")

	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	// Ensure QA is assigned.
	taskAfterQA := s.getTask(t, ctx, task.ID)
	require.NotNil(t, taskAfterQA.AssignedAgentID)
	require.Equal(t, qaID, *taskAfterQA.AssignedAgentID)

	// QA calls claim_task.
	claimed := s.claimTask(t, ctx, qaID)
	require.NotNil(t, claimed, "QA must get the assigned task")
	assert.Equal(t, task.ID, claimed.ID)

	// Agent must be marked working.
	qa, err := s.agentSvc.GetByID(ctx, qaID)
	require.NoError(t, err)
	assert.Equal(t, domainagent.StatusWorking, qa.Status)
}

func TestScenario3_QA_SweepOnRegister(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	// Submit to QA without a QA agent → task stays unassigned.
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA))

	got := s.getTask(t, ctx, task.ID)
	assert.Nil(t, got.AssignedAgentID, "pre-condition: in_qa unassigned")

	// QA agent registers → sweep picks up the waiting task.
	s.notifier.Reset()
	qaID := s.registerAgent(t, ctx, "qa")

	got = s.getTask(t, ctx, task.ID)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, qaID, *got.AssignedAgentID)
}

// ── Scenario 4: Reviewer gets task ───────────────────────────────────────────

func TestScenario4_QAPassesToReviewer_ReviewerIdle(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	qaID := s.registerAgent(t, ctx, "qa")
	reviewerID := s.registerAgent(t, ctx, "reviewer")

	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	got := s.getTask(t, ctx, task.ID)
	require.Equal(t, qaID, *got.AssignedAgentID)

	s.notifier.Reset()
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInReview)

	got = s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusInReview, got.Status)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, reviewerID, *got.AssignedAgentID)
	assert.NotEmpty(t, s.notifier.AgentNotifications(reviewerID))
}

// ── Scenario 5: Agent disconnects mid-task ────────────────────────────────────

func TestScenario5_CoderDisconnects_ReadyTaskReleased(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))

	// Coder disconnects.
	s.agentSvc.ReapOrphaned(ctx, coderID)

	agent, err := s.agentSvc.GetByID(ctx, coderID)
	require.NoError(t, err)
	assert.Equal(t, "offline", string(agent.Status))

	got := s.getTask(t, ctx, task.ID)
	assert.Nil(t, got.AssignedAgentID, "ready task must be unassigned by ReapOrphaned")
}

func TestScenario5_CoderDisconnects_InProgressTaskPreserved(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))

	s.agentSvc.ReapOrphaned(ctx, coderID)

	got := s.getTask(t, ctx, task.ID)
	// in_progress task must remain assigned (grace period).
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, coderID, *got.AssignedAgentID)
	assert.Equal(t, domaintask.StatusInProgress, got.Status)
}

// ── Scenario 6: Reconnect within grace period / grace expires ─────────────────

func TestScenario6_Reconnect_BeforeGrace_ResumesTask(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	qaID := s.registerAgent(t, ctx, "qa")
	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, qaID))

	// QA disconnects then reconnects before grace expires.
	s.agentSvc.ReapOrphaned(ctx, qaID)
	_, err := s.agentSvc.Reactivate(ctx, qaID)
	require.NoError(t, err)

	claimed := s.claimTask(t, ctx, qaID)
	require.NotNil(t, claimed, "reconnected QA must resume in_qa task")
	assert.Equal(t, task.ID, claimed.ID)
}

func TestScenario6_GraceExpires_TaskReleased(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	qaID := s.registerAgent(t, ctx, "qa")
	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, qaID))

	s.agentSvc.ReapOrphaned(ctx, qaID)

	// Grace period expires — ReleaseAgent fires.
	projectID, freed, err := s.agentSvc.ReleaseAgent(ctx, qaID)
	require.NoError(t, err)
	assert.Equal(t, s.projectID, projectID)
	assert.Contains(t, freed, domaintask.StatusInQA)

	got := s.getTask(t, ctx, task.ID)
	assert.Nil(t, got.AssignedAgentID, "task must be unassigned after grace expires")
	assert.Equal(t, domaintask.StatusInQA, got.Status, "status preserved on grace expiry")

	// Sweep picks up the now-unassigned in_qa task.
	qaID2 := s.registerAgent(t, ctx, "qa")
	got = s.getTask(t, ctx, task.ID)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, qaID2, *got.AssignedAgentID)
}

func TestScenario6_GraceExpires_ButAgentReconnected(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	qaID := s.registerAgent(t, ctx, "qa")
	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, qaID))

	// Agent reconnects BEFORE timer fires.
	_, err := s.agentSvc.Reactivate(ctx, qaID)
	require.NoError(t, err)

	// Timer fires but agent already reconnected.
	gotProject, freed, err := s.agentSvc.ReleaseAgent(ctx, qaID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, gotProject, "no release when agent already reconnected")
	assert.Nil(t, freed)
}

func TestScenario6_CoderDisconnects_InProgress_GraceExpires(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	idleCoder2 := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))

	s.agentSvc.ReapOrphaned(ctx, coderID)

	// Grace expires.
	projectID, freed, err := s.agentSvc.ReleaseAgent(ctx, coderID)
	require.NoError(t, err)
	assert.Equal(t, s.projectID, projectID)
	assert.Contains(t, freed, domaintask.StatusInProgress)

	// in_progress → reset to ready.
	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusReady, got.Status)
	assert.Nil(t, got.AssignedAgentID)

	// Sweep picks up the now-ready task.
	require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, "coder"))
	got = s.getTask(t, ctx, task.ID)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, idleCoder2, *got.AssignedAgentID)
}

// ── Scenario 7: QA/reviewer bounces task back ─────────────────────────────────

func TestScenario7_BounceBack_OriginalCoderIdle(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	qaID := s.registerAgent(t, ctx, "qa")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	require.Equal(t, qaID, *s.getTask(t, ctx, task.ID).AssignedAgentID)

	// Coder is idle (returned task via claim_task flow), QA bounces back.
	s.agentSvc.SetIdle(ctx, coderID)
	s.notifier.Reset()
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusInProgress, got.Status)
	// Original coder must be re-assigned via AssignIfIdle.
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, coderID, *got.AssignedAgentID)
	assert.NotEmpty(t, s.notifier.AgentNotifications(coderID))
}

func TestScenario7_BounceBack_OriginalCoderBusy(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	qaID := s.registerAgent(t, ctx, "qa")
	coder2ID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	require.Equal(t, qaID, *s.getTask(t, ctx, task.ID).AssignedAgentID)

	// Mark original coder as working on another task → AssignIfIdle returns false.
	dummyTask := s.createTask(t, ctx)
	require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, coderID, dummyTask.ID))

	s.notifier.Reset()
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)

	got := s.getTask(t, ctx, task.ID)
	// Must fall back to coder2 (the only idle coder).
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, coder2ID, *got.AssignedAgentID)
}

func TestScenario7_ReviewerBounce(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	_ = s.registerAgent(t, ctx, "qa")
	_ = s.registerAgent(t, ctx, "reviewer")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInReview)

	s.agentSvc.SetIdle(ctx, coderID)
	s.notifier.Reset()
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInReview, domaintask.StatusInProgress)

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusInProgress, got.Status)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, coderID, *got.AssignedAgentID)
}

func TestScenario7_BounceBack_SweepFiresForFreedFromRole(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	qaID := s.registerAgent(t, ctx, "qa")

	// task1: coder has it in_qa
	task1 := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
	require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, qaID))

	// task2: another in_qa task, unassigned
	task2 := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task2.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
	// task2 stays unassigned (no second QA)

	// QA bounces task1 back → sweep for "qa" fires → task2 gets assigned.
	s.agentSvc.SetIdle(ctx, coderID)
	s.updateStatus(t, ctx, task1.ID, domaintask.StatusInQA, domaintask.StatusInProgress)

	got2 := s.getTask(t, ctx, task2.ID)
	require.NotNil(t, got2.AssignedAgentID, "sweep after bounce-back must assign waiting in_qa task")
	assert.Equal(t, qaID, *got2.AssignedAgentID)
}

// ── Scenario 8: New task mid-production ────────────────────────────────────────

func TestScenario8_NewTask_CodersBusy(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	dummyTask := s.createTask(t, ctx)
	require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, coderID, dummyTask.ID))

	// New task arrives when coder is busy.
	newTask := s.createTask(t, ctx)
	s.updateStatus(t, ctx, newTask.ID, domaintask.StatusBacklog, domaintask.StatusReady)

	got := s.getTask(t, ctx, newTask.ID)
	assert.Equal(t, domaintask.StatusReady, got.Status)
	assert.Nil(t, got.AssignedAgentID, "task must be unassigned when all coders are working")
}

func TestScenario8_SweepPicksUpNewTask(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	_ = s.registerAgent(t, ctx, "qa")

	// Ready task waiting.
	readyTask := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, readyTask.ID, domaintask.StatusBacklog, domaintask.StatusReady))

	// Coder has an in_progress task.
	inProgressTask := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, inProgressTask.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, inProgressTask.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, inProgressTask.ID, domaintask.StatusReady, domaintask.StatusInProgress))

	// readyTask is still unassigned (coder was busy).
	got := s.getTask(t, ctx, readyTask.ID)
	assert.Nil(t, got.AssignedAgentID)

	// Coder submits → frees coder role → sweep picks up readyTask.
	s.updateStatus(t, ctx, inProgressTask.ID, domaintask.StatusInProgress, domaintask.StatusInQA)

	got = s.getTask(t, ctx, readyTask.ID)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, coderID, *got.AssignedAgentID)
}

// ── Scenario 9: Coder finishes and needs new task ─────────────────────────────

func TestScenario9_CoderFinishes_NextReadyAssigned(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	_ = s.registerAgent(t, ctx, "qa")

	task1 := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusReady, domaintask.StatusInProgress))

	task2 := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusBacklog, domaintask.StatusReady))

	s.notifier.Reset()
	s.updateStatus(t, ctx, task1.ID, domaintask.StatusInProgress, domaintask.StatusInQA)

	got2 := s.getTask(t, ctx, task2.ID)
	require.NotNil(t, got2.AssignedAgentID)
	assert.Equal(t, coderID, *got2.AssignedAgentID)
	assert.NotEmpty(t, s.notifier.AgentNotifications(coderID))
}

func TestScenario9_CoderFinishes_NoReadyTasks(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	_ = s.registerAgent(t, ctx, "qa")

	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))

	s.notifier.Reset()
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)

	// Coder calls claim_task → null (no ready tasks).
	claimed := s.claimTask(t, ctx, coderID)
	assert.Nil(t, claimed, "no ready tasks — coder should get null")

	agent, err := s.agentSvc.GetByID(ctx, coderID)
	require.NoError(t, err)
	assert.Equal(t, "idle", string(agent.Status))
}

// ── Scenario 10: QA backlog ────────────────────────────────────────────────────

func TestScenario10_MultipleCoders_QAQueue(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coder1 := s.registerAgent(t, ctx, "coder")
	coder2 := s.registerAgent(t, ctx, "coder")
	coder3 := s.registerAgent(t, ctx, "coder")
	qaID := s.registerAgent(t, ctx, "qa")
	_ = s.registerAgent(t, ctx, "reviewer")

	advance := func(coderID uuid.UUID) domaintask.Task {
		task := s.createTask(t, ctx)
		require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
		require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
		require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
		return task
	}

	task1 := advance(coder1)
	task2 := advance(coder2)
	task3 := advance(coder3)

	// All three submit to QA.
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
	require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, qaID))
	require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, qaID, task1.ID))

	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task3.ID, domaintask.StatusInProgress, domaintask.StatusInQA))

	// task1 assigned to QA, task2 and task3 unassigned.
	assert.Equal(t, qaID, *s.getTask(t, ctx, task1.ID).AssignedAgentID)
	assert.Nil(t, s.getTask(t, ctx, task2.ID).AssignedAgentID)
	assert.Nil(t, s.getTask(t, ctx, task3.ID).AssignedAgentID)

	// QA finishes task1 → sweep → picks up task2 (oldest first).
	s.agentSvc.SetIdle(ctx, qaID)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusInQA, domaintask.StatusInReview))
	require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, "qa"))

	got2 := s.getTask(t, ctx, task2.ID)
	got3 := s.getTask(t, ctx, task3.ID)
	require.NotNil(t, got2.AssignedAgentID, "task2 (older) must be picked up by sweep")
	assert.Equal(t, qaID, *got2.AssignedAgentID)
	assert.Nil(t, got3.AssignedAgentID, "task3 must remain unassigned (FIFO)")
}

func TestScenario10_WorkingStatusPreventsDoubleAssign(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	qaID := s.registerAgent(t, ctx, "qa")
	task := s.createTask(t, ctx)
	require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, qaID, task.ID))

	distSvc := distsvc.NewService(s.agentRepo)
	_, err := distSvc.Distribute(ctx, s.projectID, "qa")
	require.Error(t, err, "working QA agent must not be re-distributed")
}

// ── Scenario 11: Reviewer sweeps next in_review ────────────────────────────────

func TestScenario11_ReviewerFinishes_SweepAssignsNextInReview(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	_ = s.registerAgent(t, ctx, "qa")
	reviewerID := s.registerAgent(t, ctx, "reviewer")

	advance := func() domaintask.Task {
		task := s.createTask(t, ctx)
		require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
		require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
		require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
		s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
		s.updateStatus(t, ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInReview)
		return s.getTask(t, ctx, task.ID)
	}

	task1 := advance()
	require.Equal(t, reviewerID, *task1.AssignedAgentID)
	require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, reviewerID, task1.ID))

	// task2: in_review unassigned (reviewer was busy).
	task2 := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task2.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusInQA, domaintask.StatusInReview))
	// No reviewer available now → task2 unassigned.

	// Reviewer merges task1 → sweep for "reviewer" fires.
	s.agentSvc.SetIdle(ctx, reviewerID)
	s.updateStatus(t, ctx, task1.ID, domaintask.StatusInReview, domaintask.StatusMerged)

	got2 := s.getTask(t, ctx, task2.ID)
	require.NotNil(t, got2.AssignedAgentID, "sweep must pick up waiting in_review task")
	assert.Equal(t, reviewerID, *got2.AssignedAgentID)
}

// ── Scenario 12: Merged broadcast ────────────────────────────────────────────

func TestScenario12_MergedBroadcast(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	_ = s.registerAgent(t, ctx, "qa")
	_ = s.registerAgent(t, ctx, "reviewer")

	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInReview)

	s.notifier.Reset()
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInReview, domaintask.StatusMerged)

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusMerged, got.Status)
	assert.NotNil(t, got.CompletedAt, "completed_at must be set on merge")

	// Broadcast to coders.
	broadcasts := s.notifier.RoleNotifications("coder")
	assert.NotEmpty(t, broadcasts, "main_updated broadcast must be sent to coders")
}

// ── Scenario 13: Bounce-back with no coders → Gap H recovery ─────────────────

func TestScenario13_BounceBack_NoCoders_StrandedTaskRecovered(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	qaID := s.registerAgent(t, ctx, "qa")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, qaID))

	// Mark coder as offline (unavailable).
	require.NoError(t, s.agentRepo.UpdateStatus(ctx, coderID, "offline"))

	// QA bounces back → no idle coder → task stranded in_progress with null assigned.
	s.notifier.Reset()
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInProgress))
	// At this point sweep fires in goroutine, but finds no agents.
	require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, "coder"))

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusInProgress, got.Status)
	assert.Nil(t, got.AssignedAgentID, "task must be stranded when no coders available")

	// New coder registers → sweep picks up stranded in_progress task (Gap H fix).
	coder2 := s.registerAgent(t, ctx, "coder")

	got = s.getTask(t, ctx, task.ID)
	require.NotNil(t, got.AssignedAgentID, "sweep must recover stranded in_progress task")
	assert.Equal(t, coder2, *got.AssignedAgentID)
}

// ── Scenario 14: Startup reaper scan ─────────────────────────────────────────

func TestScenario14_StartupOrphanScan_ReleasesInflightTasks(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	idleCoderID := s.registerAgent(t, ctx, "coder")

	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))

	// Simulate process crash: coder goes offline, timer was lost.
	require.NoError(t, s.agentRepo.UpdateStatus(ctx, coderID, "offline"))

	// Startup scan.
	ids, err := s.agentSvc.ListOfflineWithInflightTasks(ctx)
	require.NoError(t, err)
	assert.Contains(t, ids, coderID)

	// Startup timer fires for each offline agent.
	for _, id := range ids {
		projectID, freed, err := s.agentSvc.ReleaseAgent(ctx, id)
		require.NoError(t, err)
		for _, status := range freed {
			role := pipeline.DefaultConfig[status].EffectiveFreedRole()
			if role != "" {
				require.NoError(t, s.taskSvc.SweepUnassigned(ctx, projectID, role))
			}
		}
	}

	got := s.getTask(t, ctx, task.ID)
	assert.Equal(t, domaintask.StatusReady, got.Status)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, idleCoderID, *got.AssignedAgentID)

	// After release, no more orphaned agents.
	ids2, err := s.agentSvc.ListOfflineWithInflightTasks(ctx)
	require.NoError(t, err)
	assert.NotContains(t, ids2, coderID)
}

func TestScenario14_StartupOrphanScan_AgentReconnectedBeforeExpiry(t *testing.T) {
	ctx := context.Background()
	s := newTestServices(t)

	coderID := s.registerAgent(t, ctx, "coder")
	task := s.createTask(t, ctx)
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
	require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
	require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
	require.NoError(t, s.agentRepo.UpdateStatus(ctx, coderID, "offline"))

	_, err := s.agentSvc.ListOfflineWithInflightTasks(ctx)
	require.NoError(t, err)

	// Agent reconnects before timer fires.
	_, err = s.agentSvc.Reactivate(ctx, coderID)
	require.NoError(t, err)

	// Timer fires but agent is no longer offline.
	gotProject, freed, err := s.agentSvc.ReleaseAgent(ctx, coderID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, gotProject)
	assert.Nil(t, freed)

	// Task must remain assigned to the reconnected agent.
	got := s.getTask(t, ctx, task.ID)
	require.NotNil(t, got.AssignedAgentID)
	assert.Equal(t, coderID, *got.AssignedAgentID)
}

// Helper: create thread for integration usage
func createTaskThread(t *testing.T, ctx context.Context, svc *threadsvc.Service, projID, taskID uuid.UUID) domainthread.Thread {
	t.Helper()
	thread, err := svc.CreateThread(ctx, projID, domainthread.TypeTask, "thread", &taskID)
	require.NoError(t, err)
	return thread
}
