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
	"github.com/alanyang/agent-mesh/internal/service/distributor"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
)

// ── helpers ───────────────────────────────────────────────────────────────────

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

// sweepNoOp allows an optional background sweep goroutine with no tasks found.
// Adds 1 to wg; the DoAndReturn calls wg.Done when the goroutine runs.
func sweepNoOp(d svcDeps, wg *sync.WaitGroup) {
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(d svcDeps) domaintask.Task
		wantErr bool
		wantMsg string
	}{
		{
			name: "success creates task and thread",
			setup: func(d svcDeps) domaintask.Task {
				projectID := uuid.New()
				created := domaintask.Task{ID: uuid.New(), ProjectID: projectID, Status: domaintask.StatusBacklog, Labels: []string{}}
				d.taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(created, nil)
				d.threadRepo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(domainthread.Thread{}, nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskCreated)).Return(nil)
				return created
			},
		},
		{
			name: "repo error",
			setup: func(d svcDeps) domaintask.Task {
				d.taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domaintask.Task{}, errors.New("db error"))
				return domaintask.Task{}
			},
			wantErr: true,
			wantMsg: "create task",
		},
		{
			// Thread creation failure is non-fatal — task is still returned.
			name: "thread creation failure is non-fatal",
			setup: func(d svcDeps) domaintask.Task {
				created := domaintask.Task{ID: uuid.New(), Labels: []string{}}
				d.taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(created, nil)
				d.threadRepo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).
					Return(domainthread.Thread{}, errors.New("thread error"))
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskCreated)).Return(nil)
				return created
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, pipeline.DefaultConfig)
			expected := tt.setup(d)

			got, err := svc.Create(context.Background(), uuid.New(), "Fix bug", "desc", domaintask.PriorityMedium, domaintask.BranchFix, "user")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, expected.ID, got.ID)
		})
	}
}

// ── GetByID / List ────────────────────────────────────────────────────────────

func TestGetByID(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(d svcDeps, taskID uuid.UUID)
		wantErr bool
	}{
		{
			name: "success",
			setup: func(d svcDeps, taskID uuid.UUID) {
				task := domaintask.Task{ID: taskID, Status: domaintask.StatusReady, Labels: []string{}}
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
			},
		},
		{
			name: "not found",
			setup: func(d svcDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(domaintask.Task{}, errors.New("not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, pipeline.DefaultConfig)
			taskID := uuid.New()
			tt.setup(d, taskID)

			got, err := svc.GetByID(context.Background(), taskID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, taskID, got.ID)
		})
	}
}

func TestList(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(d svcDeps)
		wantLen int
		wantErr bool
	}{
		{
			name: "success",
			setup: func(d svcDeps) {
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).
					Return([]domaintask.Task{newTask(domaintask.StatusReady)}, nil)
			},
			wantLen: 1,
		},
		{
			name: "repo error",
			setup: func(d svcDeps) {
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, pipeline.DefaultConfig)
			tt.setup(d)

			got, err := svc.List(context.Background(), domaintask.ListFilters{})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

// ── UpdateStatus ──────────────────────────────────────────────────────────────

// setupUpdateStatus is a function that configures mock expectations for a single
// UpdateStatus test case. It receives the deps, the pre-created taskID, and a
// WaitGroup; it calls wg.Add(1) whenever it registers a sweep locker expectation
// so the caller can wg.Wait() after invoking the service method.
type setupUpdateStatus func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup)

func TestUpdateStatus(t *testing.T) {
	tests := []struct {
		name    string
		from    domaintask.Status
		to      domaintask.Status
		cfg     pipeline.Config
		setup   setupUpdateStatus
		wantErr bool
		wantMsg string
	}{
		{
			name:    "invalid transition rejected before any repo call",
			from:    domaintask.StatusMerged,
			to:      domaintask.StatusInProgress,
			cfg:     pipeline.DefaultConfig,
			setup:   func(d svcDeps, _ uuid.UUID, _ *sync.WaitGroup) {},
			wantErr: true,
			wantMsg: "invalid transition",
		},
		{
			name: "repo UpdateStatus error",
			from: domaintask.StatusBacklog,
			to:   domaintask.StatusReady,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, _ *sync.WaitGroup) {
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusBacklog, domaintask.StatusReady).
					Return(errors.New("db error"))
			},
			wantErr: true,
		},
		{
			name: "GetByID fails after status update",
			from: domaintask.StatusBacklog,
			to:   domaintask.StatusReady,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, _ *sync.WaitGroup) {
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusBacklog, domaintask.StatusReady).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(domaintask.Task{}, errors.New("db error"))
			},
			wantErr: true,
			wantMsg: "fetch task after status update",
		},
		{
			name: "no pipeline action for target status",
			from: domaintask.StatusBacklog,
			to:   domaintask.StatusReady,
			cfg:  pipeline.Config{}, // empty config → no action
			setup: func(d svcDeps, taskID uuid.UUID, _ *sync.WaitGroup) {
				task := domaintask.Task{ID: taskID, ProjectID: uuid.New(), Labels: []string{}}
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusBacklog, domaintask.StatusReady).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
			},
		},
		{
			name: "ready→backlog has no DefaultConfig entry for backlog",
			from: domaintask.StatusReady,
			to:   domaintask.StatusBacklog,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, _ *sync.WaitGroup) {
				task := domaintask.Task{ID: taskID, ProjectID: uuid.New(), Labels: []string{}}
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusReady, domaintask.StatusBacklog).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
			},
		},
		{
			// in_progress→ready: forward assign for "coder" + sweep for freed "coder".
			name: "in_progress→ready: forward assign + freed-coder sweep",
			from: domaintask.StatusInProgress,
			to:   domaintask.StatusReady,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
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
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
		{
			// in_progress→in_qa: QA assigned + sweep for freed "coder".
			name: "pipeline assigns qa — agent available, sweep fires for coder",
			from: domaintask.StatusInProgress,
			to:   domaintask.StatusInQA,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
				task := newTask(domaintask.StatusInProgress)
				task.ID = taskID
				qaAgentID := uuid.New()
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInProgress, domaintask.StatusInQA).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "qa").Return(qaAgentID, nil)
				d.taskRepo.EXPECT().Assign(gomock.Any(), taskID, qaAgentID).Return(nil)
				d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), qaAgentID, gomock.Any()).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
		{
			// No QA available: graceful no-op, no service error.
			name: "pipeline assigns qa — no agent available",
			from: domaintask.StatusInProgress,
			to:   domaintask.StatusInQA,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
				task := newTask(domaintask.StatusInProgress)
				task.ID = taskID
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInProgress, domaintask.StatusInQA).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "qa").Return(uuid.Nil, distributor.ErrNoAgentAvailable)
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
		{
			// Unexpected distribute error: logged and ignored, no service error.
			name: "pipeline assign — unexpected distributor error is non-fatal",
			from: domaintask.StatusInProgress,
			to:   domaintask.StatusInQA,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
				task := newTask(domaintask.StatusInProgress)
				task.ID = taskID
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInProgress, domaintask.StatusInQA).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "qa").Return(uuid.Nil, errors.New("unexpected"))
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
		{
			// in_qa→in_progress: original coder idle → AssignIfIdle succeeds.
			name: "bounce-back from in_qa — original coder idle",
			from: domaintask.StatusInQA,
			to:   domaintask.StatusInProgress,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
				coderID := uuid.New()
				task := newTask(domaintask.StatusInQA)
				task.ID = taskID
				task.CoderID = &coderID
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), taskID, coderID).Return(true, nil)
				d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), coderID, gomock.Any()).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
		{
			// in_qa→in_progress: original coder busy → fallback Distribute.
			name: "bounce-back from in_qa — original coder busy, fallback distribute",
			from: domaintask.StatusInQA,
			to:   domaintask.StatusInProgress,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
				coderID := uuid.New()
				newCoder := uuid.New()
				task := newTask(domaintask.StatusInQA)
				task.ID = taskID
				task.CoderID = &coderID
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), taskID, coderID).Return(false, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "coder").Return(newCoder, nil)
				d.taskRepo.EXPECT().Assign(gomock.Any(), taskID, newCoder).Return(nil)
				d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), newCoder, gomock.Any()).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
		{
			// in_qa→in_progress: no CoderID → skip AssignIfIdle, go straight to Distribute.
			name: "bounce-back from in_qa — no CoderID, distribute directly",
			from: domaintask.StatusInQA,
			to:   domaintask.StatusInProgress,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
				agentID := uuid.New()
				task := newTask(domaintask.StatusInQA)
				task.ID = taskID
				// task.CoderID is nil
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "coder").Return(agentID, nil)
				d.taskRepo.EXPECT().Assign(gomock.Any(), taskID, agentID).Return(nil)
				d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agentID, gomock.Any()).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
		{
			// in_review→in_progress: same bounce-back path as in_qa→in_progress.
			name: "bounce-back from in_review — original coder idle",
			from: domaintask.StatusInReview,
			to:   domaintask.StatusInProgress,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
				coderID := uuid.New()
				task := newTask(domaintask.StatusInReview)
				task.ID = taskID
				task.CoderID = &coderID
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInReview, domaintask.StatusInProgress).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), taskID, coderID).Return(true, nil)
				d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), coderID, gomock.Any()).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
		{
			name: "bounce-back AssignIfIdle repo error",
			from: domaintask.StatusInQA,
			to:   domaintask.StatusInProgress,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, _ *sync.WaitGroup) {
				coderID := uuid.New()
				task := newTask(domaintask.StatusInQA)
				task.ID = taskID
				task.CoderID = &coderID
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), taskID, coderID).Return(false, errors.New("db error"))
			},
			wantErr: true,
			wantMsg: "bounce-back preferred assign",
		},
		{
			name: "bounce-back fallback assign repo error",
			from: domaintask.StatusInQA,
			to:   domaintask.StatusInProgress,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, _ *sync.WaitGroup) {
				coderID := uuid.New()
				newAgentID := uuid.New()
				task := newTask(domaintask.StatusInQA)
				task.ID = taskID
				task.CoderID = &coderID
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInProgress).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.taskRepo.EXPECT().AssignIfIdle(gomock.Any(), taskID, coderID).Return(false, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), task.ProjectID, "coder").Return(newAgentID, nil)
				d.taskRepo.EXPECT().Assign(gomock.Any(), taskID, newAgentID).Return(errors.New("db error"))
			},
			wantErr: true,
		},
		{
			name: "in_review→merged: broadcast to coder role",
			from: domaintask.StatusInReview,
			to:   domaintask.StatusMerged,
			cfg:  pipeline.DefaultConfig,
			setup: func(d svcDeps, taskID uuid.UUID, wg *sync.WaitGroup) {
				task := newTask(domaintask.StatusInReview)
				task.ID = taskID
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInReview, domaintask.StatusMerged).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.roleNotifier.EXPECT().NotifyProjectRole(gomock.Any(), task.ProjectID, "coder", gomock.Any()).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskCompleted)).Return(nil)
				wg.Add(1)
				sweepNoOp(d, wg)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, tt.cfg)
			taskID := uuid.New()
			var wg sync.WaitGroup
			tt.setup(d, taskID, &wg)

			err := svc.UpdateStatus(context.Background(), taskID, tt.from, tt.to)
			wg.Wait()

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantMsg != "" {
					assert.Contains(t, err.Error(), tt.wantMsg)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

// ── SweepUnassigned ───────────────────────────────────────────────────────────

func TestSweepUnassigned(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		setup   func(d svcDeps, projectID uuid.UUID)
		wantErr bool
	}{
		{
			name: "one task one agent — assigned and notified",
			role: "qa",
			setup: func(d svcDeps, projectID uuid.UUID) {
				task := newTask(domaintask.StatusInQA)
				agentID := uuid.New()
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{task}, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(agentID, nil)
				d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, agentID).Return(nil)
				d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agentID, gomock.Any()).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil)
			},
		},
		{
			name: "no tasks — no-op",
			role: "qa",
			setup: func(d svcDeps, projectID uuid.UUID) {
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()
			},
		},
		{
			name: "no agents — no-op",
			role: "qa",
			setup: func(d svcDeps, projectID uuid.UUID) {
				task := newTask(domaintask.StatusInQA)
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{task}, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(uuid.Nil, distributor.ErrNoAgentAvailable)
			},
		},
		{
			name: "3 tasks 2 agents — third stays unassigned",
			role: "qa",
			setup: func(d svcDeps, projectID uuid.UUID) {
				tasks := []domaintask.Task{newTask(domaintask.StatusInQA), newTask(domaintask.StatusInQA), newTask(domaintask.StatusInQA)}
				agent1, agent2 := uuid.New(), uuid.New()
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
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
			},
		},
		{
			name: "agent runs dry mid-batch",
			role: "qa",
			setup: func(d svcDeps, projectID uuid.UUID) {
				tasks := []domaintask.Task{newTask(domaintask.StatusInQA), newTask(domaintask.StatusInQA), newTask(domaintask.StatusInQA)}
				agent1 := uuid.New()
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(tasks, nil)
				gomock.InOrder(
					d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(agent1, nil),
					d.taskRepo.EXPECT().Assign(gomock.Any(), tasks[0].ID, agent1).Return(nil),
					d.agentNotifier.EXPECT().NotifyAgent(gomock.Any(), agent1, gomock.Any()).Return(nil),
					d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskAssigned)).Return(nil),
					d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(uuid.Nil, distributor.ErrNoAgentAvailable),
				)
			},
		},
		{
			name: "locker error returned",
			role: "qa",
			setup: func(d svcDeps, projectID uuid.UUID) {
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("lock error"))
			},
			wantErr: true,
		},
		{
			name: "list error returned",
			role: "qa",
			setup: func(d svcDeps, projectID uuid.UUID) {
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))
			},
			wantErr: true,
		},
		{
			name: "assign error returned",
			role: "qa",
			setup: func(d svcDeps, projectID uuid.UUID) {
				task := newTask(domaintask.StatusInQA)
				agentID := uuid.New()
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{task}, nil)
				d.dist.EXPECT().Distribute(gomock.Any(), projectID, "qa").Return(agentID, nil)
				d.taskRepo.EXPECT().Assign(gomock.Any(), task.ID, agentID).Return(errors.New("assign error"))
			},
			wantErr: true,
		},
		{
			// Gap H: stranded in_progress task recovered when "coder" sweep runs.
			name: "recovers stranded in_progress orphan (Gap H)",
			role: "coder",
			setup: func(d svcDeps, projectID uuid.UUID) {
				task := newTask(domaintask.StatusInProgress)
				coderID := uuid.New()
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
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
			},
		},
		{
			// No idle coders: stranded in_progress sweep is a no-op.
			name: "stranded in_progress — no idle coder available",
			role: "coder",
			setup: func(d svcDeps, projectID uuid.UUID) {
				task := newTask(domaintask.StatusInProgress)
				d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error { return fn(ctx) })
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, f domaintask.ListFilters) ([]domaintask.Task, error) {
						if f.Status != nil && *f.Status == domaintask.StatusInProgress {
							return []domaintask.Task{task}, nil
						}
						return []domaintask.Task{}, nil
					}).AnyTimes()
				d.dist.EXPECT().Distribute(gomock.Any(), projectID, "coder").Return(uuid.Nil, distributor.ErrNoAgentAvailable)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, pipeline.DefaultConfig)
			projectID := uuid.New()
			tt.setup(d, projectID)

			err := svc.SweepUnassigned(context.Background(), projectID, tt.role)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// ── SetPRUrl ──────────────────────────────────────────────────────────────────

func TestSetPRUrl(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(d svcDeps, taskID uuid.UUID)
		wantErr bool
	}{
		{
			name: "success",
			setup: func(d svcDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().SetPRUrl(gomock.Any(), taskID, "https://gh.com/pr/1").Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeTaskUpdated)).Return(nil)
			},
		},
		{
			name: "repo error",
			setup: func(d svcDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().SetPRUrl(gomock.Any(), taskID, gomock.Any()).Return(errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, pipeline.DefaultConfig)
			taskID := uuid.New()
			tt.setup(d, taskID)

			err := svc.SetPRUrl(context.Background(), taskID, "https://gh.com/pr/1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// ── AddDependency ─────────────────────────────────────────────────────────────

func TestAddDependency(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(d svcDeps, taskID, depID uuid.UUID)
		wantErr bool
	}{
		{
			name: "success",
			setup: func(d svcDeps, taskID, depID uuid.UUID) {
				d.taskRepo.EXPECT().AddDependency(gomock.Any(), domaintask.Dependency{TaskID: taskID, DependsOnID: depID}).Return(nil)
			},
		},
		{
			name: "repo error",
			setup: func(d svcDeps, taskID, depID uuid.UUID) {
				d.taskRepo.EXPECT().AddDependency(gomock.Any(), gomock.Any()).Return(errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, pipeline.DefaultConfig)
			taskID, depID := uuid.New(), uuid.New()
			tt.setup(d, taskID, depID)

			err := svc.AddDependency(context.Background(), taskID, depID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// ── RemoveDependency ──────────────────────────────────────────────────────────

func TestRemoveDependency(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(d svcDeps, taskID, depID uuid.UUID)
		wantErr bool
	}{
		{
			name: "success",
			setup: func(d svcDeps, taskID, depID uuid.UUID) {
				d.taskRepo.EXPECT().RemoveDependency(gomock.Any(), taskID, depID).Return(nil)
			},
		},
		{
			name: "repo error",
			setup: func(d svcDeps, taskID, depID uuid.UUID) {
				d.taskRepo.EXPECT().RemoveDependency(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, pipeline.DefaultConfig)
			taskID, depID := uuid.New(), uuid.New()
			tt.setup(d, taskID, depID)

			err := svc.RemoveDependency(context.Background(), taskID, depID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// ── GetDependencies ───────────────────────────────────────────────────────────

func TestGetDependencies(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(d svcDeps, taskID uuid.UUID)
		wantLen int
		wantErr bool
	}{
		{
			name: "success",
			setup: func(d svcDeps, taskID uuid.UUID) {
				deps := []domaintask.Task{newTask(domaintask.StatusMerged)}
				d.taskRepo.EXPECT().GetDependencies(gomock.Any(), taskID).Return(deps, nil)
			},
			wantLen: 1,
		},
		{
			name: "repo error",
			setup: func(d svcDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().GetDependencies(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t, pipeline.DefaultConfig)
			taskID := uuid.New()
			tt.setup(d, taskID)

			got, err := svc.GetDependencies(context.Background(), taskID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}
