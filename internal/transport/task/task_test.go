package task_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	"github.com/alanyang/agent-mesh/internal/mocks"
	"github.com/alanyang/agent-mesh/internal/service/distributor"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	transporttask "github.com/alanyang/agent-mesh/internal/transport/task"
)

func init() { gin.SetMode(gin.TestMode) }

type taskDeps struct {
	taskRepo      *mocks.MockTaskRepository
	bus           *mocks.MockEventBus
	dist          *mocks.MockDistributor
	threadRepo    *mocks.MockThreadRepository
	agentNotifier *mocks.MockAgentNotifier
	roleNotifier  *mocks.MockRoleNotifier
	locker        *mocks.MockAdvisoryLocker
}

func newTaskSvc(t *testing.T) (*tasksvc.Service, taskDeps) {
	t.Helper()
	ctrl := gomock.NewController(t)
	d := taskDeps{
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
		d.agentNotifier, d.roleNotifier, pipeline.DefaultConfig, d.locker,
	)
	return svc, d
}

func newRouter(svc *tasksvc.Service) *gin.Engine {
	r := gin.New()
	transporttask.Register(r.Group("/tasks"), svc)
	return r
}

// allowSweep silently absorbs any background sweep goroutine calls.
func allowSweep(d taskDeps) {
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		}).AnyTimes()
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()
	d.dist.EXPECT().Distribute(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(uuid.Nil, distributor.ErrNoAgentAvailable).AnyTimes()
}

// ── POST / (createTask) ───────────────────────────────────────────────────────

func TestCreateTask(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]interface{}
		setup    func(d taskDeps)
		wantCode int
	}{
		{
			name: "success returns 201",
			body: map[string]interface{}{
				"project_id":  uuid.New().String(),
				"title":       "Fix bug",
				"priority":    "medium",
				"branch_type": "fix",
				"created_by":  "user",
			},
			setup: func(d taskDeps) {
				created := domaintask.Task{ID: uuid.New(), Labels: []string{}}
				d.taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(created, nil)
				d.threadRepo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(domainthread.Thread{}, nil)
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
			},
			wantCode: http.StatusCreated,
		},
		{
			name:     "missing required fields returns 400",
			body:     map[string]interface{}{},
			setup:    func(d taskDeps) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t)
			tt.setup(d)
			r := newRouter(svc)

			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/tasks/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}

// ── GET / (listTasks) ─────────────────────────────────────────────────────────

func TestListTasks(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		setup    func(d taskDeps)
		wantCode int
	}{
		{
			name: "success returns 200",
			setup: func(d taskDeps) {
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).
					Return([]domaintask.Task{{ID: uuid.New(), Labels: []string{}}}, nil)
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid project_id returns 400",
			query:    "?project_id=not-a-uuid",
			setup:    func(d taskDeps) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid assigned_to returns 400",
			query:    "?assigned_to=not-a-uuid",
			setup:    func(d taskDeps) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t)
			tt.setup(d)
			r := newRouter(svc)

			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/tasks/"+tt.query, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}

// ── GET /:id (getTask) ────────────────────────────────────────────────────────

func TestGetTask(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		setup    func(d taskDeps, taskID uuid.UUID)
		wantCode int
	}{
		{
			name: "success returns 200",
			setup: func(d taskDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).
					Return(domaintask.Task{ID: taskID, Labels: []string{}}, nil)
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid UUID returns 400",
			id:       "not-a-uuid",
			setup:    func(d taskDeps, taskID uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "not found returns 404",
			setup: func(d taskDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).
					Return(domaintask.Task{}, errors.New("task not found"))
			},
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t)
			taskID := uuid.New()
			tt.setup(d, taskID)
			r := newRouter(svc)

			id := tt.id
			if id == "" {
				id = taskID.String()
			}

			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/tasks/"+id, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}

// ── PATCH /:id (updateTaskStatus) ────────────────────────────────────────────

func TestUpdateTaskStatus(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		body     map[string]string
		setup    func(d taskDeps, taskID uuid.UUID)
		wantCode int
	}{
		{
			name: "success returns 200",
			body: map[string]string{"status_from": "in_qa", "status_to": "in_review"},
			setup: func(d taskDeps, taskID uuid.UUID) {
				projectID := uuid.New()
				task := domaintask.Task{ID: taskID, ProjectID: projectID, Status: domaintask.StatusInReview, Labels: []string{}}
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInReview).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil).AnyTimes()
				allowSweep(d)
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid transition returns 409",
			body:     map[string]string{"status_from": "merged", "status_to": "in_progress"},
			setup:    func(d taskDeps, taskID uuid.UUID) {},
			wantCode: http.StatusConflict,
		},
		{
			name: "db error returns 500",
			body: map[string]string{"status_from": "backlog", "status_to": "ready"},
			setup: func(d taskDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusBacklog, domaintask.StatusReady).
					Return(errors.New("unexpected db failure"))
			},
			wantCode: http.StatusInternalServerError,
		},
		{
			name:     "missing required fields returns 400",
			body:     map[string]string{},
			setup:    func(d taskDeps, taskID uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid task UUID returns 400",
			id:       "not-a-uuid",
			body:     map[string]string{"status_from": "backlog", "status_to": "ready"},
			setup:    func(d taskDeps, taskID uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t)
			taskID := uuid.New()
			tt.setup(d, taskID)
			r := newRouter(svc)

			id := tt.id
			if id == "" {
				id = taskID.String()
			}

			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, "/tasks/"+id, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}

// ── POST /:id/dependencies ────────────────────────────────────────────────────

func TestAddDependency(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]string
		setup    func(d taskDeps, taskID, depID uuid.UUID)
		wantCode int
	}{
		{
			name: "success returns 201",
			setup: func(d taskDeps, taskID, depID uuid.UUID) {
				d.taskRepo.EXPECT().AddDependency(gomock.Any(),
					domaintask.Dependency{TaskID: taskID, DependsOnID: depID}).Return(nil)
			},
			wantCode: http.StatusCreated,
		},
		{
			name:     "missing depends_on_id returns 400",
			body:     map[string]string{},
			setup:    func(d taskDeps, taskID, depID uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t)
			taskID, depID := uuid.New(), uuid.New()
			tt.setup(d, taskID, depID)
			r := newRouter(svc)

			bodyMap := tt.body
			if bodyMap == nil {
				bodyMap = map[string]string{"depends_on_id": depID.String()}
			}
			body, _ := json.Marshal(bodyMap)
			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
				"/tasks/"+taskID.String()+"/dependencies", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}

// ── DELETE /:id/dependencies/:depId ──────────────────────────────────────────

func TestRemoveDependency(t *testing.T) {
	tests := []struct {
		name     string
		depID    string
		setup    func(d taskDeps, taskID, depID uuid.UUID)
		wantCode int
	}{
		{
			name: "success returns 204",
			setup: func(d taskDeps, taskID, depID uuid.UUID) {
				d.taskRepo.EXPECT().RemoveDependency(gomock.Any(), taskID, depID).Return(nil)
			},
			wantCode: http.StatusNoContent,
		},
		{
			name:     "invalid dep UUID returns 400",
			depID:    "not-a-uuid",
			setup:    func(d taskDeps, taskID, depID uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newTaskSvc(t)
			taskID, depID := uuid.New(), uuid.New()
			tt.setup(d, taskID, depID)
			r := newRouter(svc)

			depIDStr := tt.depID
			if depIDStr == "" {
				depIDStr = depID.String()
			}

			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
				"/tasks/"+taskID.String()+"/dependencies/"+depIDStr, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}
