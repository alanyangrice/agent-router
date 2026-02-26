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

func TestCreateThread(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]interface{}
		setup    func(repo *mocks.MockThreadRepository)
		wantCode int
	}{
		{
			name: "success returns 201 with TypeTask",
			body: map[string]interface{}{"project_id": uuid.New().String(), "name": "thread"},
			setup: func(repo *mocks.MockThreadRepository) {
				projectID := uuid.New()
				created := domainthread.Thread{ID: uuid.New(), ProjectID: projectID, Type: domainthread.TypeTask, Name: "thread"}
				repo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(created, nil)
			},
			wantCode: http.StatusCreated,
		},
		{
			name:     "missing required fields returns 400",
			body:     map[string]interface{}{},
			setup:    func(repo *mocks.MockThreadRepository) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newThreadSvc(t)
			tt.setup(repo)
			r := newRouter(svc)

			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/threads/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			if tt.wantCode == http.StatusCreated {
				var got domainthread.Thread
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
				assert.Equal(t, domainthread.TypeTask, got.Type)
			}
		})
	}
}

// ── GET / (listThreads) ───────────────────────────────────────────────────────

func TestListThreads(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		setup    func(repo *mocks.MockThreadRepository)
		wantCode int
	}{
		{
			name: "success returns 200",
			setup: func(repo *mocks.MockThreadRepository) {
				repo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).
					Return([]domainthread.Thread{{ID: uuid.New()}}, nil)
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid project_id returns 400",
			query:    "?project_id=not-a-uuid",
			setup:    func(repo *mocks.MockThreadRepository) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid task_id returns 400",
			query:    "?task_id=not-a-uuid",
			setup:    func(repo *mocks.MockThreadRepository) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newThreadSvc(t)
			tt.setup(repo)
			r := newRouter(svc)

			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/threads/"+tt.query, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}

// ── GET /:id/messages (listMessages) ─────────────────────────────────────────

func TestListMessages(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		setup    func(repo *mocks.MockThreadRepository, threadID uuid.UUID)
		wantCode int
	}{
		{
			name: "success returns 200",
			setup: func(repo *mocks.MockThreadRepository, threadID uuid.UUID) {
				repo.EXPECT().ListMessages(gomock.Any(), threadID).
					Return([]domainthread.Message{{ID: uuid.New()}}, nil)
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid UUID returns 400",
			id:       "not-a-uuid",
			setup:    func(repo *mocks.MockThreadRepository, threadID uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newThreadSvc(t)
			threadID := uuid.New()
			tt.setup(repo, threadID)
			r := newRouter(svc)

			id := tt.id
			if id == "" {
				id = threadID.String()
			}

			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/threads/"+id+"/messages", nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}

// ── POST /:id/messages (postMessage) ─────────────────────────────────────────

func TestPostMessage(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]string
		setup    func(repo *mocks.MockThreadRepository, bus *mocks.MockEventBus, threadID uuid.UUID)
		wantCode int
	}{
		{
			name: "success returns 201",
			body: map[string]string{"post_type": "progress", "content": "hello"},
			setup: func(repo *mocks.MockThreadRepository, bus *mocks.MockEventBus, threadID uuid.UUID) {
				expected := domainthread.Message{ID: uuid.New(), ThreadID: threadID, Content: "hello"}
				repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(expected, nil)
				bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
			},
			wantCode: http.StatusCreated,
		},
		{
			name:     "missing required fields returns 400",
			body:     map[string]string{},
			setup:    func(repo *mocks.MockThreadRepository, bus *mocks.MockEventBus, threadID uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "service error returns 500",
			body: map[string]string{"post_type": "progress", "content": "hi"},
			setup: func(repo *mocks.MockThreadRepository, bus *mocks.MockEventBus, threadID uuid.UUID) {
				repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).
					Return(domainthread.Message{}, errors.New("db error"))
			},
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, bus := newThreadSvc(t)
			threadID := uuid.New()
			tt.setup(repo, bus, threadID)
			r := newRouter(svc)

			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/threads/"+threadID.String()+"/messages", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			if tt.wantCode == http.StatusCreated {
				var got domainthread.Message
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
				assert.NotEqual(t, uuid.Nil, got.ID)
			}
		})
	}
}
