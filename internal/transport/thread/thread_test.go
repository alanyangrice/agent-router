package thread_test

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

	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	"github.com/alanyang/agent-mesh/internal/mocks"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
	transportthread "github.com/alanyang/agent-mesh/internal/transport/thread"
)

func init() { gin.SetMode(gin.TestMode) }

func newRouter(svc *threadsvc.Service) *gin.Engine {
	r := gin.New()
	transportthread.Register(r.Group("/threads"), svc)
	return r
}

func newThreadSvc(t *testing.T) (*threadsvc.Service, *mocks.MockThreadRepository, *mocks.MockEventBus) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mocks.NewMockThreadRepository(ctrl)
	bus := mocks.NewMockEventBus(ctrl)
	return threadsvc.NewService(repo, bus), repo, bus
}

// ── POST / (createThread) ─────────────────────────────────────────────────────

func TestCreateThread_Success(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	r := newRouter(svc)
	projectID := uuid.New()

	created := domainthread.Thread{ID: uuid.New(), ProjectID: projectID, Type: domainthread.TypeTask, Name: "thread"}
	repo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(created, nil)

	body, _ := json.Marshal(map[string]interface{}{"project_id": projectID.String(), "name": "thread"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/threads/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var got domainthread.Thread
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	// Handler always passes TypeTask regardless of any type field in body.
	assert.Equal(t, domainthread.TypeTask, got.Type)
}

func TestCreateThread_BadBody(t *testing.T) {
	svc, _, _ := newThreadSvc(t)
	r := newRouter(svc)

	body, _ := json.Marshal(map[string]string{}) // missing required name/project_id
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/threads/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── GET / (listThreads) ────────────────────────────────────────────────────────

func TestListThreads_Success(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	r := newRouter(svc)

	repo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{{ID: uuid.New()}}, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/threads/", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListThreads_InvalidProjectID(t *testing.T) {
	svc, _, _ := newThreadSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/threads/?project_id=not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListThreads_InvalidTaskID(t *testing.T) {
	svc, _, _ := newThreadSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/threads/?task_id=not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── GET /:id/messages (listMessages) ─────────────────────────────────────────

func TestListMessages_Success(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	r := newRouter(svc)
	threadID := uuid.New()

	repo.EXPECT().ListMessages(gomock.Any(), threadID).Return([]domainthread.Message{{ID: uuid.New()}}, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/threads/"+threadID.String()+"/messages", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListMessages_InvalidID(t *testing.T) {
	svc, _, _ := newThreadSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/threads/not-a-uuid/messages", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── POST /:id/messages (postMessage) ─────────────────────────────────────────

func TestPostMessage_Success(t *testing.T) {
	svc, repo, bus := newThreadSvc(t)
	r := newRouter(svc)
	threadID := uuid.New()

	expected := domainthread.Message{ID: uuid.New(), ThreadID: threadID, Content: "hello"}
	repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(expected, nil)
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	body, _ := json.Marshal(map[string]string{"post_type": "progress", "content": "hello"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/threads/"+threadID.String()+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var got domainthread.Message
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, expected.ID, got.ID)
}

func TestPostMessage_BadBody(t *testing.T) {
	svc, _, _ := newThreadSvc(t)
	r := newRouter(svc)
	threadID := uuid.New()

	body, _ := json.Marshal(map[string]string{}) // missing required fields
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/threads/"+threadID.String()+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPostMessage_ServiceError(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	r := newRouter(svc)
	threadID := uuid.New()

	repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(domainthread.Message{}, errors.New("db error"))

	body, _ := json.Marshal(map[string]string{"post_type": "progress", "content": "hi"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/threads/"+threadID.String()+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
