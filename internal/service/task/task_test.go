package task_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	"github.com/alanyang/agent-mesh/internal/mocks"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	"github.com/alanyang/agent-mesh/internal/service/distributor"
)

// ── test helpers ──────────────────────────────────────────────────────────────

type svcDeps struct {
	taskRepo      *mocks.MockTaskRepository
	bus           *mocks.MockEventBus
	dist          *mocks.MockDistributor
	threadRepo    *mocks.MockThreadRepository
	agentNotifier *mocks.MockAgentNotifier
	roleNotifier  *mocks.MockRoleNotifier
	locker        *mocks.MockAdvisoryLocker
}

func newTaskSvc(t *testing.T, cfg pipeline.Config) (*tasksvc.Service, svcDeps) {
	t.Helper()
	ctrl := gomock.NewController(t)
	d := svcDeps{
		taskRepo:      mocks.NewMockTaskRepository(ctrl),
		bus:           mocks.NewMockEventBus(ctrl),
		dist:          mocks.NewMockDistributor(ctrl),
		threadRepo:    mocks.NewMockThreadRepository(ctrl),
		agentNotifier: mocks.NewMockAgentNotifier(ctrl),
		roleNotifier:  mocks.NewMockRoleNotifier(ctrl),
		locker:        mocks.NewMockAdvisoryLocker(ctrl),
	}
	svc := tasksvc.NewService(
		d.taskRepo, d.bus, d.dist, d.threadRepo,
		d.agentNotifier, d.roleNotifier, cfg, d.locker,
	)
	return svc, d
}

// syncLocker makes locker.WithLock execute the callback synchronously.
// Returns a WaitGroup that callers can Wait on to ensure the goroutine completes.
func syncLocker(d svcDeps) *sync.WaitGroup {
	var wg sync.WaitGroup
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		}).AnyTimes()
	return &wg
}

func newTask(status domaintask.Status) domaintask.Task {
	pid := uuid.New()
	return domaintask.Task{
		ID:        uuid.New(),
		ProjectID: pid,
		Status:    status,
		Labels:    []string{},
	}
}

func matchEventType(et event.Type) gomock.Matcher {
	return eventTypeMatcher{et}
}

type eventTypeMatcher struct{ want event.Type }

func (m eventTypeMatcher) Matches(x interface{}) bool {
	e, ok := x.(event.Event)
	return ok && e.Type == m.want
}
func (m eventTypeMatcher) String() string { return "event.Type=" + string(m.want) }

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate_Success(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()

	created := domaintask.Task{ID: uuid.New(), ProjectID: projectID, Status: domaintask.StatusBacklog, Labels: []string{}}
	d.taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(created, nil)
	d.threadRepo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(domainthread.Thread{}, nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskCreated)).Return(nil)

	got, err := svc.Create(ctx, projectID, "Fix bug", "desc", domaintask.PriorityMedium, domaintask.BranchFix, "user")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
}

func TestCreate_RepoError(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)

	d.taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domaintask.Task{}, errors.New("db error"))

	_, err := svc.Create(context.Background(), uuid.New(), "title", "desc", domaintask.PriorityLow, domaintask.BranchFix, "user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create task")
}

func TestCreate_ThreadCreationFails(t *testing.T) {
	// Non-fatal: task is returned even if thread creation fails.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)

	created := domaintask.Task{ID: uuid.New(), Labels: []string{}}
	d.taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(created, nil)
	d.threadRepo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(domainthread.Thread{}, errors.New("thread error"))
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskCreated)).Return(nil)

	got, err := svc.Create(context.Background(), uuid.New(), "title", "", domaintask.PriorityLow, domaintask.BranchFix, "user")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
}

// ── GetByID / List ────────────────────────────────────────────────────────────

func TestGetByID_Success(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusReady)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)

	got, err := svc.GetByID(context.Background(), task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.ID, got.ID)
}

func TestGetByID_NotFound(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID := uuid.New()
	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(domaintask.Task{}, errors.New("not found"))

	_, err := svc.GetByID(context.Background(), taskID)
	require.Error(t, err)
}

func TestList_Success(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	tasks := []domaintask.Task{newTask(domaintask.StatusReady)}
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(tasks, nil)

	got, err := svc.List(context.Background(), domaintask.ListFilters{})
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestList_Error(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))

	_, err := svc.List(context.Background(), domaintask.ListFilters{})
	require.Error(t, err)
}

// ── UpdateStatus — invalid transition ────────────────────────────────────────

func TestUpdateStatus_InvalidTransition(t *testing.T) {
	svc, _ := newTaskSvc(t, pipeline.DefaultConfig)

	err := svc.UpdateStatus(context.Background(), uuid.New(), domaintask.StatusMerged, domaintask.StatusInProgress)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")
}

// ── UpdateStatus — repo/fetch error paths ────────────────────────────────────

func TestUpdateStatus_RepoUpdateFails(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID := uuid.New()

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusBacklog, domaintask.StatusReady).
		Return(errors.New("db error"))

	err := svc.UpdateStatus(context.Background(), taskID, domaintask.StatusBacklog, domaintask.StatusReady)
	require.Error(t, err)
}

func TestUpdateStatus_GetByIDFails(t *testing.T) {
	// TypeTaskUpdated IS published before GetByID fails.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID := uuid.New()

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusBacklog, domaintask.StatusReady).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(domaintask.Task{}, errors.New("db error"))

	err := svc.UpdateStatus(context.Background(), taskID, domaintask.StatusBacklog, domaintask.StatusReady)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch task after status update")
}

func TestUpdateStatus_BounceBack_AssignIfIdleError(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInQA)
	coderID := uuid.New()
	task.CoderID = &coderID

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), task.ID, coderID).Return(false, errors.New("db error"))

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bounce-back preferred assign")
}

func TestUpdateStatus_BounceBack_FallbackAssignFails(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInQA)
	coderID := uuid.New()
	task.CoderID = &coderID
	newAgentID := uuid.New()

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), task.ID, coderID).Return(false, nil) // coder busy
	d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "coder").Return(newAgentID, nil)
	d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, newAgentID).Return(errors.New("db error"))

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)
	require.Error(t, err)
}

// ── UpdateStatus — no pipeline action ────────────────────────────────────────

func TestUpdateStatus_NoPipelineAction(t *testing.T) {
	// backlog→ready: pipelineConfig[ready].AssignRole="coder" — assign fires.
	// Use a config with no entry for statusReady to test the "no action" path.
	emptyCfg := pipeline.Config{}
	svc, d := newTaskSvc(t, emptyCfg)
	taskID := uuid.New()
	task := domaintask.Task{ID: taskID, ProjectID: uuid.New(), Labels: []string{}}

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusBacklog, domaintask.StatusReady).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)

	err := svc.UpdateStatus(context.Background(), taskID, domaintask.StatusBacklog, domaintask.StatusReady)
	require.NoError(t, err)
}

func TestUpdateStatus_ReadyToBacklog(t *testing.T) {
	// ready→backlog: valid domain transition; no DefaultConfig entry for backlog.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID := uuid.New()
	task := domaintask.Task{ID: taskID, ProjectID: uuid.New(), Labels: []string{}}

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusReady, domaintask.StatusBacklog).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)

	err := svc.UpdateStatus(context.Background(), taskID, domaintask.StatusReady, domaintask.StatusBacklog)
	require.NoError(t, err)
}

func TestUpdateStatus_InProgressToReady_DirectTransition(t *testing.T) {
	// in_progress→ready: NOT a bounce-back (from is neither InQA nor InReview).
	// pipelineConfig[ready].AssignRole="coder" → Distribute called.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID := uuid.New()
	agentID := uuid.New()
	projectID := uuid.New()
	task := domaintask.Task{ID: taskID, ProjectID: projectID, Labels: []string{}}

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInProgress, domaintask.StatusReady).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
	d.dist.EXPECT().Distribute(gomock.Any(), projectID, "coder").Return(agentID, nil)
	d.taskRepo.EXPECT().Assign(gomock.Any(), taskID, agentID).Return(nil)
	d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agentID, gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
	// pipelineConfig[in_progress].FreedRole="coder" → sweep goroutine fires.
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), taskID, domaintask.StatusInProgress, domaintask.StatusReady)
	wg.Wait()
	require.NoError(t, err)
}

// ── UpdateStatus — pipeline assigns a role ────────────────────────────────────

func TestUpdateStatus_PipelineAssignRole_AgentAvailable(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInProgress)
	qaAgentID := uuid.New()

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInProgress, domaintask.StatusInQA).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "qa").Return(qaAgentID, nil)
	d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, qaAgentID).Return(nil)
	d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), qaAgentID, gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
	// FreedRole sweep for "coder" (leaving in_progress)
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	wg.Wait()
	require.NoError(t, err)
}

func TestUpdateStatus_PipelineAssignRole_NoAgent(t *testing.T) {
	// Graceful: no agent available — no assign, no notify, no error.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInProgress)

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInProgress, domaintask.StatusInQA).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "qa").Return(uuid.Nil, distributor.ErrNoAgentAvailable)
	// Sweep for freed coder role.
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	wg.Wait()
	require.NoError(t, err)
}

func TestUpdateStatus_PipelineAssignRole_DistributeError(t *testing.T) {
	// Unexpected distributor error: logged, task stays unassigned, no service error.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInProgress)

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInProgress, domaintask.StatusInQA).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "qa").Return(uuid.Nil, errors.New("unexpected"))
	// Sweep for freed coder role.
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInProgress, domaintask.StatusInQA)
	wg.Wait()
	require.NoError(t, err)
}

// ── UpdateStatus — bounce-back routing ───────────────────────────────────────

func TestUpdateStatus_BounceBack_OriginalCoderIdle(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInQA)
	coderID := uuid.New()
	task.CoderID = &coderID

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), task.ID, coderID).Return(true, nil)
	// Notify original coder with "task_returned".
	d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), coderID, gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
	// Sweep for freed QA slot (from=InQA).
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)
	wg.Wait()
	require.NoError(t, err)
}

func TestUpdateStatus_BounceBack_OriginalCoderBusy(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInQA)
	coderID := uuid.New()
	task.CoderID = &coderID
	newCoder := uuid.New()

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), task.ID, coderID).Return(false, nil) // coder busy
	d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "coder").Return(newCoder, nil)
	d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, newCoder).Return(nil)
	d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), newCoder, gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
	// Sweep for freed QA slot.
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)
	wg.Wait()
	require.NoError(t, err)
}

func TestUpdateStatus_BounceBack_NoCoderID(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInQA)
	// task.CoderID is nil
	agentID := uuid.New()

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	// No CoderID: AssignIfIdle NOT called; Distribute called directly.
	d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "coder").Return(agentID, nil)
	d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, agentID).Return(nil)
	d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agentID, gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
	// Sweep for freed QA slot.
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInQA, domaintask.StatusInProgress)
	wg.Wait()
	require.NoError(t, err)
}

func TestUpdateStatus_BounceBack_FromInReview_OriginalCoderIdle(t *testing.T) {
	// Verifies in_review → in_progress uses the same bounce-back path as in_qa → in_progress.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInReview)
	coderID := uuid.New()
	task.CoderID = &coderID

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInReview, domaintask.StatusInProgress).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), task.ID, coderID).Return(true, nil)
	d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), coderID, gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
	// Sweep for freed reviewer slot (from=InReview).
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInReview, domaintask.StatusInProgress)
	wg.Wait()
	require.NoError(t, err)
}

// ── UpdateStatus — merged broadcast ──────────────────────────────────────────

func TestUpdateStatus_MergedBroadcast(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	task := newTask(domaintask.StatusInReview)

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), task.ID, domaintask.StatusInReview, domaintask.StatusMerged).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
	d.taskRepo.EXPECT().GetByID(gomock.Any(), task.ID).Return(task, nil)
	d.roleNotifier.EXPECT().NotifyProjectRole(gomock.Any(), task.ProjectID, "coder", gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskCompleted)).Return(nil)
	// Sweep for freed reviewer slot (from=InReview).
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, key int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.UpdateStatus(context.Background(), task.ID, domaintask.StatusInReview, domaintask.StatusMerged)
	wg.Wait()
	require.NoError(t, err)
}

// ── SweepUnassigned ───────────────────────────────────────────────────────────

func TestSweepUnassigned_OneTask_OneAgent(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()
	task := newTask(domaintask.StatusInQA)
	agentID := uuid.New()

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{task}, nil)
	d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(agentID, nil)
	d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, agentID).Return(nil)
	d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agentID, gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)

	err := svc.SweepUnassigned(ctx, projectID, "qa")
	require.NoError(t, err)
}

func TestSweepUnassigned_MultipleTasksMultipleAgents(t *testing.T) {
	// 3 tasks, 2 agents: exactly 2 get assigned, third stays unassigned.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()
	tasks := []domaintask.Task{newTask(domaintask.StatusInQA), newTask(domaintask.StatusInQA), newTask(domaintask.StatusInQA)}
	agent1, agent2 := uuid.New(), uuid.New()

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(tasks, nil)
	gomock.InOrder(
		d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(agent1, nil),
		d.taskRepo.EXPECT().Assign(gomock.Any(), tasks[0].ID, agent1).Return(nil),
		d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agent1, gomock.Any()).Return(nil),
		d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil),
		d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(agent2, nil),
		d.taskRepo.EXPECT().Assign(gomock.Any(), tasks[1].ID, agent2).Return(nil),
		d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agent2, gomock.Any()).Return(nil),
		d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil),
		d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(uuid.Nil, distributor.ErrNoAgentAvailable),
	)

	err := svc.SweepUnassigned(ctx, projectID, "qa")
	require.NoError(t, err)
}

func TestSweepUnassigned_NoTasks(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	err := svc.SweepUnassigned(ctx, projectID, "qa")
	require.NoError(t, err)
}

func TestSweepUnassigned_NoAgents(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()
	task := newTask(domaintask.StatusInQA)

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{task}, nil)
	d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(uuid.Nil, distributor.ErrNoAgentAvailable)

	err := svc.SweepUnassigned(ctx, projectID, "qa")
	require.NoError(t, err)
}

func TestSweepUnassigned_AgentRunsDry_MidBatch(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()
	tasks := []domaintask.Task{newTask(domaintask.StatusInQA), newTask(domaintask.StatusInQA), newTask(domaintask.StatusInQA)}
	agent1 := uuid.New()

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(tasks, nil)
	gomock.InOrder(
		d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(agent1, nil),
		d.taskRepo.EXPECT().Assign(gomock.Any(), tasks[0].ID, agent1).Return(nil),
		d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agent1, gomock.Any()).Return(nil),
		d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil),
		d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(uuid.Nil, distributor.ErrNoAgentAvailable),
	)

	err := svc.SweepUnassigned(ctx, projectID, "qa")
	require.NoError(t, err)
}

func TestSweepUnassigned_LockerError(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("lock error"))

	err := svc.SweepUnassigned(ctx, uuid.New(), "qa")
	require.Error(t, err)
}

func TestSweepUnassigned_ListError(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))

	err := svc.SweepUnassigned(ctx, uuid.New(), "qa")
	require.Error(t, err)
}

func TestSweepUnassigned_AssignError(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()
	task := newTask(domaintask.StatusInQA)
	agentID := uuid.New()

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{task}, nil)
	d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(agentID, nil)
	d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, agentID).Return(errors.New("assign error"))

	err := svc.SweepUnassigned(ctx, projectID, "qa")
	require.Error(t, err)
}

// ── SweepUnassigned — Gap H (in_progress orphan recovery) ────────────────────

func TestSweepUnassigned_RecoversBouncebackOrphan(t *testing.T) {
	// pipelineConfig[in_progress].FreedRole="coder" — stranded in_progress task is swept.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()
	task := newTask(domaintask.StatusInProgress)
	coderID := uuid.New()

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	// List may be called for multiple statuses matching "coder"; return task for in_progress.
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, f domaintask.ListFilters) ([]domaintask.Task, error) {
			if f.Status != nil && *f.Status == domaintask.StatusInProgress {
				return []domaintask.Task{task}, nil
			}
			return []domaintask.Task{}, nil
		}).AnyTimes()
	d.dist.EXPECT().Distribute(gomock.Any(), projectID, "coder").Return(coderID, nil)
	d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, coderID).Return(nil)
	d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), coderID, gomock.Any()).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)

	err := svc.SweepUnassigned(ctx, projectID, "coder")
	require.NoError(t, err)
}

func TestSweepUnassigned_InProgressOrphan_NoAgent(t *testing.T) {
	// No idle coders: sweep is no-op, returns nil.
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	ctx := context.Background()
	projectID := uuid.New()
	task := newTask(domaintask.StatusInProgress)

	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, f domaintask.ListFilters) ([]domaintask.Task, error) {
			if f.Status != nil && *f.Status == domaintask.StatusInProgress {
				return []domaintask.Task{task}, nil
			}
			return []domaintask.Task{}, nil
		}).AnyTimes()
	d.dist.EXPECT().Distribute(gomock.Any(), projectID, "coder").Return(uuid.Nil, distributor.ErrNoAgentAvailable)

	err := svc.SweepUnassigned(ctx, projectID, "coder")
	require.NoError(t, err)
}

// ── Dependency helpers ────────────────────────────────────────────────────────

func TestSetPRUrl_Success(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID := uuid.New()
	d.taskRepo.EXPECT().SetPRUrl(gomock.Any(), taskID, "https://gh.com/pr/1").Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)

	err := svc.SetPRUrl(context.Background(), taskID, "https://gh.com/pr/1")
	require.NoError(t, err)
}

func TestSetPRUrl_Error(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID := uuid.New()
	d.taskRepo.EXPECT().SetPRUrl(gomock.Any(), taskID, gomock.Any()).Return(errors.New("db error"))

	err := svc.SetPRUrl(context.Background(), taskID, "https://gh.com/pr/1")
	require.Error(t, err)
}

func TestAddDependency_Success(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID, depID := uuid.New(), uuid.New()
	d.taskRepo.EXPECT().AddDependency(gomock.Any(), domaintask.Dependency{TaskID: taskID, DependsOnID: depID}).Return(nil)

	err := svc.AddDependency(context.Background(), taskID, depID)
	require.NoError(t, err)
}

func TestAddDependency_Error(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	d.taskRepo.EXPECT().AddDependency(gomock.Any(), gomock.Any()).Return(errors.New("db error"))

	err := svc.AddDependency(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
}

func TestRemoveDependency_Success(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID, depID := uuid.New(), uuid.New()
	d.taskRepo.EXPECT().RemoveDependency(gomock.Any(), taskID, depID).Return(nil)

	err := svc.RemoveDependency(context.Background(), taskID, depID)
	require.NoError(t, err)
}

func TestRemoveDependency_Error(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	d.taskRepo.EXPECT().RemoveDependency(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("db error"))

	err := svc.RemoveDependency(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
}

func TestGetDependencies_Success(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	taskID := uuid.New()
	deps := []domaintask.Task{newTask(domaintask.StatusMerged)}
	d.taskRepo.EXPECT().GetDependencies(gomock.Any(), taskID).Return(deps, nil)

	got, err := svc.GetDependencies(context.Background(), taskID)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestGetDependencies_Error(t *testing.T) {
	svc, d := newTaskSvc(t, pipeline.DefaultConfig)
	d.taskRepo.EXPECT().GetDependencies(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))

	_, err := svc.GetDependencies(context.Background(), uuid.New())
	require.Error(t, err)
}
