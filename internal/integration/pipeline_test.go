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

// createTaskThread creates a thread linked to a task.
func createTaskThread(t *testing.T, ctx context.Context, svc *threadsvc.Service, projID, taskID uuid.UUID) domainthread.Thread {
	t.Helper()
	thread, err := svc.CreateThread(ctx, projID, domainthread.TypeTask, "thread", &taskID)
	require.NoError(t, err)
	return thread
}

// ── TestPipelineScenarios ─────────────────────────────────────────────────────

func TestPipelineScenarios(t *testing.T) {

	// ── Scenario 1: Task first distributed ───────────────────────────────────

	t.Run("Scenario1_TaskDistribution", func(t *testing.T) {
		t.Run("TaskDistributed_AgentIdle", func(t *testing.T) {
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
		})

		t.Run("TaskDistributed_NoAgent", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)
			task := s.createTask(t, ctx)

			s.updateStatus(t, ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady)

			got := s.getTask(t, ctx, task.ID)
			assert.Equal(t, domaintask.StatusReady, got.Status)
			assert.Nil(t, got.AssignedAgentID, "task must stay unassigned when no agents available")
		})

		t.Run("SweepOnCoderRegister", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)
			task := s.createTask(t, ctx)

			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			got := s.getTask(t, ctx, task.ID)
			assert.Nil(t, got.AssignedAgentID)

			coderID := s.registerAgent(t, ctx, "coder")

			got = s.getTask(t, ctx, task.ID)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, coderID, *got.AssignedAgentID)
		})
	})

	// ── Scenario 2: Coder submits to QA ──────────────────────────────────────

	t.Run("Scenario2_CoderSubmitsToQA", func(t *testing.T) {
		t.Run("QAIdle", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			qaID := s.registerAgent(t, ctx, "qa")
			task := s.createTask(t, ctx)

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
		})

		t.Run("NoQA", func(t *testing.T) {
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
		})

		t.Run("SweepTriggeredForCoderRole", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			task1 := s.createTask(t, ctx)
			task2 := s.createTask(t, ctx)

			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusReady, domaintask.StatusInProgress))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusBacklog, domaintask.StatusReady))

			s.notifier.Reset()
			s.updateStatus(t, ctx, task1.ID, domaintask.StatusInProgress, domaintask.StatusInQA)

			got2 := s.getTask(t, ctx, task2.ID)
			require.NotNil(t, got2.AssignedAgentID, "sweep must assign waiting ready task to freed coder")
			assert.Equal(t, coderID, *got2.AssignedAgentID)
		})
	})

	// ── Scenario 3: QA claims task ────────────────────────────────────────────

	t.Run("Scenario3_QAClaimsTask", func(t *testing.T) {
		t.Run("ClaimTask_AfterAssignment", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			qaID := s.registerAgent(t, ctx, "qa")

			task := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
			s.updateStatus(t, ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
			taskAfterQA := s.getTask(t, ctx, task.ID)
			require.NotNil(t, taskAfterQA.AssignedAgentID)
			require.Equal(t, qaID, *taskAfterQA.AssignedAgentID)

			claimed := s.claimTask(t, ctx, qaID)
			require.NotNil(t, claimed, "QA must get the assigned task")
			assert.Equal(t, task.ID, claimed.ID)

			qa, err := s.agentSvc.GetByID(ctx, qaID)
			require.NoError(t, err)
			assert.Equal(t, domainagent.StatusWorking, qa.Status)
		})

		t.Run("SweepOnRegister", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			task := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusInProgress, domaintask.StatusInQA))

			got := s.getTask(t, ctx, task.ID)
			assert.Nil(t, got.AssignedAgentID, "pre-condition: in_qa unassigned")

			s.notifier.Reset()
			qaID := s.registerAgent(t, ctx, "qa")

			got = s.getTask(t, ctx, task.ID)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, qaID, *got.AssignedAgentID)
		})
	})

	// ── Scenario 4: Reviewer gets task ───────────────────────────────────────

	t.Run("Scenario4_ReviewerGetsTask", func(t *testing.T) {
		t.Run("QAPassesToReviewer_ReviewerIdle", func(t *testing.T) {
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
		})
	})

	// ── Scenario 5: Agent disconnects mid-task ────────────────────────────────

	t.Run("Scenario5_AgentDisconnects", func(t *testing.T) {
		t.Run("CoderDisconnects_ReadyTaskReleased", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			task := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))

			s.agentSvc.ReapOrphaned(ctx, coderID)

			agent, err := s.agentSvc.GetByID(ctx, coderID)
			require.NoError(t, err)
			assert.Equal(t, "offline", string(agent.Status))

			got := s.getTask(t, ctx, task.ID)
			assert.Nil(t, got.AssignedAgentID, "ready task must be unassigned by ReapOrphaned")
		})

		t.Run("CoderDisconnects_InProgressTaskPreserved", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			task := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))

			s.agentSvc.ReapOrphaned(ctx, coderID)

			got := s.getTask(t, ctx, task.ID)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, coderID, *got.AssignedAgentID)
			assert.Equal(t, domaintask.StatusInProgress, got.Status)
		})
	})

	// ── Scenario 6: Reconnect within grace period / grace expires ─────────────

	t.Run("Scenario6_GracePeriod", func(t *testing.T) {
		t.Run("Reconnect_BeforeGrace_ResumesTask", func(t *testing.T) {
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
			_, err := s.agentSvc.Reactivate(ctx, qaID)
			require.NoError(t, err)

			claimed := s.claimTask(t, ctx, qaID)
			require.NotNil(t, claimed, "reconnected QA must resume in_qa task")
			assert.Equal(t, task.ID, claimed.ID)
		})

		t.Run("GraceExpires_TaskReleased", func(t *testing.T) {
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

			projectID, freed, err := s.agentSvc.ReleaseAgent(ctx, qaID)
			require.NoError(t, err)
			assert.Equal(t, s.projectID, projectID)
			assert.Contains(t, freed, domaintask.StatusInQA)

			got := s.getTask(t, ctx, task.ID)
			assert.Nil(t, got.AssignedAgentID, "task must be unassigned after grace expires")
			assert.Equal(t, domaintask.StatusInQA, got.Status, "status preserved on grace expiry")

			qaID2 := s.registerAgent(t, ctx, "qa")
			got = s.getTask(t, ctx, task.ID)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, qaID2, *got.AssignedAgentID)
		})

		t.Run("GraceExpires_ButAgentReconnected", func(t *testing.T) {
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

			_, err := s.agentSvc.Reactivate(ctx, qaID)
			require.NoError(t, err)

			gotProject, freed, err := s.agentSvc.ReleaseAgent(ctx, qaID)
			require.NoError(t, err)
			assert.Equal(t, uuid.Nil, gotProject, "no release when agent already reconnected")
			assert.Nil(t, freed)
		})

		t.Run("CoderDisconnects_InProgress_GraceExpires", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			idleCoder2 := s.registerAgent(t, ctx, "coder")
			task := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))

			s.agentSvc.ReapOrphaned(ctx, coderID)

			projectID, freed, err := s.agentSvc.ReleaseAgent(ctx, coderID)
			require.NoError(t, err)
			assert.Equal(t, s.projectID, projectID)
			assert.Contains(t, freed, domaintask.StatusInProgress)

			got := s.getTask(t, ctx, task.ID)
			assert.Equal(t, domaintask.StatusReady, got.Status)
			assert.Nil(t, got.AssignedAgentID)

			require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, "coder"))
			got = s.getTask(t, ctx, task.ID)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, idleCoder2, *got.AssignedAgentID)
		})
	})

	// ── Scenario 7: QA/reviewer bounces task back ─────────────────────────────

	t.Run("Scenario7_BounceBack", func(t *testing.T) {
		t.Run("OriginalCoderIdle", func(t *testing.T) {
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

			s.agentSvc.SetIdle(ctx, coderID)
			s.notifier.Reset()
			s.updateStatus(t, ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)

			got := s.getTask(t, ctx, task.ID)
			assert.Equal(t, domaintask.StatusInProgress, got.Status)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, coderID, *got.AssignedAgentID)
			assert.NotEmpty(t, s.notifier.AgentNotifications(coderID))
		})

		t.Run("OriginalCoderBusy", func(t *testing.T) {
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

			dummyTask := s.createTask(t, ctx)
			require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, coderID, dummyTask.ID))

			s.notifier.Reset()
			s.updateStatus(t, ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)

			got := s.getTask(t, ctx, task.ID)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, coder2ID, *got.AssignedAgentID)
		})

		t.Run("ReviewerBounce", func(t *testing.T) {
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
		})

		t.Run("SweepFiresForFreedFromRole", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			qaID := s.registerAgent(t, ctx, "qa")

			task1 := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusReady, domaintask.StatusInProgress))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
			require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, qaID))

			task2 := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task2.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusReady, domaintask.StatusInProgress))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusInProgress, domaintask.StatusInQA))

			s.agentSvc.SetIdle(ctx, coderID)
			s.updateStatus(t, ctx, task1.ID, domaintask.StatusInQA, domaintask.StatusInProgress)

			got2 := s.getTask(t, ctx, task2.ID)
			require.NotNil(t, got2.AssignedAgentID, "sweep after bounce-back must assign waiting in_qa task")
			assert.Equal(t, qaID, *got2.AssignedAgentID)
		})
	})

	// ── Scenario 8: New task mid-production ──────────────────────────────────

	t.Run("Scenario8_NewTaskMidProduction", func(t *testing.T) {
		t.Run("CodersBusy_TaskStaysUnassigned", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			dummyTask := s.createTask(t, ctx)
			require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, coderID, dummyTask.ID))

			newTask := s.createTask(t, ctx)
			s.updateStatus(t, ctx, newTask.ID, domaintask.StatusBacklog, domaintask.StatusReady)

			got := s.getTask(t, ctx, newTask.ID)
			assert.Equal(t, domaintask.StatusReady, got.Status)
			assert.Nil(t, got.AssignedAgentID, "task must be unassigned when all coders are working")
		})

		t.Run("SweepPicksUpNewTask", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			_ = s.registerAgent(t, ctx, "qa")

			readyTask := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, readyTask.ID, domaintask.StatusBacklog, domaintask.StatusReady))

			inProgressTask := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, inProgressTask.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, inProgressTask.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, inProgressTask.ID, domaintask.StatusReady, domaintask.StatusInProgress))

			got := s.getTask(t, ctx, readyTask.ID)
			assert.Nil(t, got.AssignedAgentID)

			s.updateStatus(t, ctx, inProgressTask.ID, domaintask.StatusInProgress, domaintask.StatusInQA)

			got = s.getTask(t, ctx, readyTask.ID)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, coderID, *got.AssignedAgentID)
		})
	})

	// ── Scenario 9: Coder finishes and needs new task ─────────────────────────

	t.Run("Scenario9_CoderFinishes", func(t *testing.T) {
		t.Run("NextReadyAssigned", func(t *testing.T) {
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
		})

		t.Run("NoReadyTasks_CoderGoesIdle", func(t *testing.T) {
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

			claimed := s.claimTask(t, ctx, coderID)
			assert.Nil(t, claimed, "no ready tasks — coder should get null")

			agent, err := s.agentSvc.GetByID(ctx, coderID)
			require.NoError(t, err)
			assert.Equal(t, "idle", string(agent.Status))
		})
	})

	// ── Scenario 10: QA backlog ───────────────────────────────────────────────

	t.Run("Scenario10_QABacklog", func(t *testing.T) {
		t.Run("MultipleCoders_QAQueue_FIFO", func(t *testing.T) {
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

			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
			require.NoError(t, s.taskRepo.Assign(ctx, task1.ID, qaID))
			require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, qaID, task1.ID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task3.ID, domaintask.StatusInProgress, domaintask.StatusInQA))

			assert.Equal(t, qaID, *s.getTask(t, ctx, task1.ID).AssignedAgentID)
			assert.Nil(t, s.getTask(t, ctx, task2.ID).AssignedAgentID)
			assert.Nil(t, s.getTask(t, ctx, task3.ID).AssignedAgentID)

			s.agentSvc.SetIdle(ctx, qaID)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task1.ID, domaintask.StatusInQA, domaintask.StatusInReview))
			require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, "qa"))

			got2 := s.getTask(t, ctx, task2.ID)
			got3 := s.getTask(t, ctx, task3.ID)
			require.NotNil(t, got2.AssignedAgentID, "task2 (older) must be picked up by sweep")
			assert.Equal(t, qaID, *got2.AssignedAgentID)
			assert.Nil(t, got3.AssignedAgentID, "task3 must remain unassigned (FIFO)")
		})

		t.Run("WorkingStatusPreventsDoubleAssign", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			qaID := s.registerAgent(t, ctx, "qa")
			task := s.createTask(t, ctx)
			require.NoError(t, s.agentRepo.SetWorkingStatus(ctx, qaID, task.ID))

			distSvcInstance := distsvc.NewService(s.agentRepo)
			_, err := distSvcInstance.Distribute(ctx, s.projectID, "qa")
			require.Error(t, err, "working QA agent must not be re-distributed")
		})
	})

	// ── Scenario 11: Reviewer sweeps next in_review ───────────────────────────

	t.Run("Scenario11_ReviewerFinishes_SweepAssignsNextInReview", func(t *testing.T) {
		t.Run("SweepPicksUpWaitingInReview", func(t *testing.T) {
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

			task2 := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task2.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusReady, domaintask.StatusInProgress))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusInProgress, domaintask.StatusInQA))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task2.ID, domaintask.StatusInQA, domaintask.StatusInReview))

			s.agentSvc.SetIdle(ctx, reviewerID)
			s.updateStatus(t, ctx, task1.ID, domaintask.StatusInReview, domaintask.StatusMerged)

			got2 := s.getTask(t, ctx, task2.ID)
			require.NotNil(t, got2.AssignedAgentID, "sweep must pick up waiting in_review task")
			assert.Equal(t, reviewerID, *got2.AssignedAgentID)
		})
	})

	// ── Scenario 12: Merged broadcast ────────────────────────────────────────

	t.Run("Scenario12_MergedBroadcast", func(t *testing.T) {
		t.Run("BroadcastSentToCoders", func(t *testing.T) {
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

			broadcasts := s.notifier.RoleNotifications("coder")
			assert.NotEmpty(t, broadcasts, "main_updated broadcast must be sent to coders")
		})
	})

	// ── Scenario 13: Bounce-back with no coders → Gap H recovery ─────────────

	t.Run("Scenario13_BounceBack_NoCoders_GapHRecovery", func(t *testing.T) {
		t.Run("StrandedTaskRecoveredByNewCoder", func(t *testing.T) {
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

			require.NoError(t, s.agentRepo.UpdateStatus(ctx, coderID, "offline"))

			s.notifier.Reset()
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusInQA, domaintask.StatusInProgress))
			require.NoError(t, s.taskSvc.SweepUnassigned(ctx, s.projectID, "coder"))

			got := s.getTask(t, ctx, task.ID)
			assert.Equal(t, domaintask.StatusInProgress, got.Status)
			assert.Nil(t, got.AssignedAgentID, "task must be stranded when no coders available")

			coder2 := s.registerAgent(t, ctx, "coder")

			got = s.getTask(t, ctx, task.ID)
			require.NotNil(t, got.AssignedAgentID, "sweep must recover stranded in_progress task")
			assert.Equal(t, coder2, *got.AssignedAgentID)
		})
	})

	// ── Scenario 14: Startup reaper scan ─────────────────────────────────────

	t.Run("Scenario14_StartupOrphanScan", func(t *testing.T) {
		t.Run("ReleasesInflightTasks", func(t *testing.T) {
			ctx := context.Background()
			s := newTestServices(t)

			coderID := s.registerAgent(t, ctx, "coder")
			idleCoderID := s.registerAgent(t, ctx, "coder")

			task := s.createTask(t, ctx)
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusBacklog, domaintask.StatusReady))
			require.NoError(t, s.taskRepo.Assign(ctx, task.ID, coderID))
			require.NoError(t, s.taskSvc.UpdateStatus(ctx, task.ID, domaintask.StatusReady, domaintask.StatusInProgress))

			require.NoError(t, s.agentRepo.UpdateStatus(ctx, coderID, "offline"))

			ids, err := s.agentSvc.ListOfflineWithInflightTasks(ctx)
			require.NoError(t, err)
			assert.Contains(t, ids, coderID)

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

			ids2, err := s.agentSvc.ListOfflineWithInflightTasks(ctx)
			require.NoError(t, err)
			assert.NotContains(t, ids2, coderID)
		})

		t.Run("AgentReconnectedBeforeExpiry_NoRelease", func(t *testing.T) {
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

			_, err = s.agentSvc.Reactivate(ctx, coderID)
			require.NoError(t, err)

			gotProject, freed, err := s.agentSvc.ReleaseAgent(ctx, coderID)
			require.NoError(t, err)
			assert.Equal(t, uuid.Nil, gotProject)
			assert.Nil(t, freed)

			got := s.getTask(t, ctx, task.ID)
			require.NotNil(t, got.AssignedAgentID)
			assert.Equal(t, coderID, *got.AssignedAgentID)
		})
	})
}
