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
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	"github.com/alanyang/agent-mesh/internal/mocks"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	"github.com/alanyang/agent-mesh/internal/service/distributor"
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

// allowSweep makes the locker accept any sweep goroutine calls silently
// and allows distributor to return no-agent (graceful no-op).
func allowSweep(d taskDeps) {
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		}).AnyTimes()
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()
	d.dist.EXPECT().Distribute(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(uuid.Nil, distributor.ErrNoAgentAvailable).AnyTimes()
}

// ── POST / (createTask) ────────────────────────────────────────────────────────

func TestCreateTask_Success(t *testing.T) {
	svc, d := newTaskSvc(t)
	r := newRouter(svc)
	projectID := uuid.New()

	created := domaintask.Task{ID: uuid.New(), ProjectID: projectID, Labels: []string{}}
	d.taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(created, nil)
	d.threadRepo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(domainthread.Thread{}, nil)
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	body, _ := json.Marshal(map[string]interface{}{
		"project_id":  projectID.String(),
		"title":       "Fix bug",
		"priority":    "medium",
		"branch_type": "fix",
		"created_by":  "user",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/tasks/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var got domaintask.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, created.ID, got.ID)
}

func TestCreateTask_BadBody(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)

	body, _ := json.Marshal(map[string]string{}) // missing required fields
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/tasks/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── GET / (listTasks) ─────────────────────────────────────────────────────────

func TestListTasks_Success(t *testing.T) {
	svc, d := newTaskSvc(t)
	r := newRouter(svc)

	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{{ID: uuid.New(), Labels: []string{}}}, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/tasks/", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListTasks_InvalidProjectID(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/tasks/?project_id=not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListTasks_InvalidAssignedTo(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/tasks/?assigned_to=not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── GET /:id (getTask) ────────────────────────────────────────────────────────

func TestGetTask_Success(t *testing.T) {
	svc, d := newTaskSvc(t)
	r := newRouter(svc)
	taskID := uuid.New()

	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(domaintask.Task{ID: taskID, Labels: []string{}}, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/tasks/"+taskID.String(), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetTask_InvalidID(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/tasks/not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetTask_NotFound(t *testing.T) {
	svc, d := newTaskSvc(t)
	r := newRouter(svc)
	taskID := uuid.New()

	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(domaintask.Task{}, errors.New("task not found"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/tasks/"+taskID.String(), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── PATCH /:id (updateTaskStatus) ────────────────────────────────────────────

func TestUpdateTaskStatus_Success(t *testing.T) {
	svc, d := newTaskSvc(t)
	r := newRouter(svc)
	taskID := uuid.New()
	projectID := uuid.New()
	// Use in_qa→in_review so no bounce-back special case, and reviewer gets assigned (or not).
	task := domaintask.Task{ID: taskID, ProjectID: projectID, Status: domaintask.StatusInReview, Labels: []string{}}

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInReview).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil).AnyTimes()
	allowSweep(d) // handles sweep goroutine + dist calls

	body, _ := json.Marshal(map[string]string{"status_from": "in_qa", "status_to": "in_review"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, "/tasks/"+taskID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUpdateTaskStatus_InvalidTransition_Returns409(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)
	taskID := uuid.New()

	body, _ := json.Marshal(map[string]string{"status_from": "merged", "status_to": "in_progress"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, "/tasks/"+taskID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestUpdateTaskStatus_ServiceError_Returns500(t *testing.T) {
	// Non-transition error (e.g. DB failure) → 500.
	svc, d := newTaskSvc(t)
	r := newRouter(svc)
	taskID := uuid.New()

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusBacklog, domaintask.StatusReady).
		Return(errors.New("unexpected db failure"))

	body, _ := json.Marshal(map[string]string{"status_from": "backlog", "status_to": "ready"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, "/tasks/"+taskID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdateTaskStatus_BadBody(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)
	taskID := uuid.New()

	body, _ := json.Marshal(map[string]string{}) // missing required fields
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, "/tasks/"+taskID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateTaskStatus_InvalidID(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)

	body, _ := json.Marshal(map[string]string{"status_from": "backlog", "status_to": "ready"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, "/tasks/not-a-uuid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── POST /:id/dependencies ────────────────────────────────────────────────────

func TestAddDependency_Success(t *testing.T) {
	svc, d := newTaskSvc(t)
	r := newRouter(svc)
	taskID, depID := uuid.New(), uuid.New()

	d.taskRepo.EXPECT().AddDependency(gomock.Any(), domaintask.Dependency{TaskID: taskID, DependsOnID: depID}).Return(nil)

	body, _ := json.Marshal(map[string]string{"depends_on_id": depID.String()})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/tasks/"+taskID.String()+"/dependencies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestAddDependency_BadBody(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)
	taskID := uuid.New()

	body, _ := json.Marshal(map[string]string{}) // missing depends_on_id
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/tasks/"+taskID.String()+"/dependencies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── DELETE /:id/dependencies/:depId ──────────────────────────────────────────

func TestRemoveDependency_Success(t *testing.T) {
	svc, d := newTaskSvc(t)
	r := newRouter(svc)
	taskID, depID := uuid.New(), uuid.New()

	d.taskRepo.EXPECT().RemoveDependency(gomock.Any(), taskID, depID).Return(nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
		"/tasks/"+taskID.String()+"/dependencies/"+depID.String(), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestRemoveDependency_InvalidDepID(t *testing.T) {
	svc, _ := newTaskSvc(t)
	r := newRouter(svc)
	taskID := uuid.New()

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
		"/tasks/"+taskID.String()+"/dependencies/not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
