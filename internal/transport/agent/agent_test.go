package agent_test

import (
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

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	"github.com/alanyang/agent-mesh/internal/mocks"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	transportagent "github.com/alanyang/agent-mesh/internal/transport/agent"
)

func init() { gin.SetMode(gin.TestMode) }

func newRouter(svc *agentsvc.Service) *gin.Engine {
	r := gin.New()
	transportagent.Register(r.Group("/agents"), svc)
	return r
}

func newAgentSvc(t *testing.T) (*agentsvc.Service, *mocks.MockAgentRepository, *mocks.MockTaskRepository, *mocks.MockEventBus) {
	t.Helper()
	ctrl := gomock.NewController(t)
	agentRepo := mocks.NewMockAgentRepository(ctrl)
	taskRepo := mocks.NewMockTaskRepository(ctrl)
	bus := mocks.NewMockEventBus(ctrl)
	svc := agentsvc.NewService(agentRepo, taskRepo, bus)
	return svc, agentRepo, taskRepo, bus
}

// ── GET / (listAgents) ────────────────────────────────────────────────────────

func TestListAgents_Success(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	r := newRouter(svc)

	agents := []domainagent.Agent{{ID: uuid.New(), Role: "coder"}}
	agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(agents, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var got []domainagent.Agent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Len(t, got, 1)
}

func TestListAgents_WithFilters(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	r := newRouter(svc)
	projectID := uuid.New()

	agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, f domainagent.ListFilters) ([]domainagent.Agent, error) {
			require.NotNil(t, f.ProjectID)
			assert.Equal(t, projectID, *f.ProjectID)
			require.NotNil(t, f.Role)
			assert.Equal(t, "coder", *f.Role)
			require.NotNil(t, f.Status)
			assert.Equal(t, domainagent.StatusIdle, *f.Status)
			return []domainagent.Agent{}, nil
		})

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"/agents/?project_id="+projectID.String()+"&role=coder&status=idle", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListAgents_InvalidProjectID(t *testing.T) {
	svc, _, _, _ := newAgentSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/?project_id=not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListAgents_ServiceError(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	r := newRouter(svc)

	agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── GET /:id (getAgent) ───────────────────────────────────────────────────────

func TestGetAgent_Success(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	r := newRouter(svc)
	agentID := uuid.New()

	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{ID: agentID, Role: "qa"}, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/"+agentID.String(), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var got domainagent.Agent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, agentID, got.ID)
}

func TestGetAgent_InvalidID(t *testing.T) {
	svc, _, _, _ := newAgentSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetAgent_NotFound(t *testing.T) {
	// Service returns a "not found" error → handler maps to 404.
	svc, agentRepo, _, _ := newAgentSvc(t)
	r := newRouter(svc)
	agentID := uuid.New()

	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{}, errors.New("agent not found"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/"+agentID.String(), nil)
	r.ServeHTTP(w, req)
	// Handler returns 404 for all GetByID errors.
	assert.Equal(t, http.StatusNotFound, w.Code)
}
