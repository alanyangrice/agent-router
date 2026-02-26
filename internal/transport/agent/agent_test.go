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

func TestListAgents(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		setup    func(agentRepo *mocks.MockAgentRepository)
		wantCode int
		wantLen  int
	}{
		{
			name: "success returns agents",
			setup: func(agentRepo *mocks.MockAgentRepository) {
				agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).
					Return([]domainagent.Agent{{ID: uuid.New(), Role: "coder"}}, nil)
			},
			wantCode: http.StatusOK,
			wantLen:  1,
		},
		{
			name:  "with valid filters passes them to service",
			query: "?role=coder&status=idle",
			setup: func(agentRepo *mocks.MockAgentRepository) {
				agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, f domainagent.ListFilters) ([]domainagent.Agent, error) {
						require.NotNil(t, f.Role)
						assert.Equal(t, "coder", *f.Role)
						require.NotNil(t, f.Status)
						assert.Equal(t, domainagent.StatusIdle, *f.Status)
						return []domainagent.Agent{}, nil
					})
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid project_id returns 400",
			query:    "?project_id=not-a-uuid",
			setup:    func(agentRepo *mocks.MockAgentRepository) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "service error returns 500",
			setup: func(agentRepo *mocks.MockAgentRepository) {
				agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))
			},
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, _ := newAgentSvc(t)
			tt.setup(agentRepo)
			r := newRouter(svc)

			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/"+tt.query, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			if tt.wantLen > 0 {
				var got []domainagent.Agent
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
				assert.Len(t, got, tt.wantLen)
			}
		})
	}
}

// ── GET /:id (getAgent) ───────────────────────────────────────────────────────

func TestGetAgent(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		setup    func(agentRepo *mocks.MockAgentRepository, agentID uuid.UUID)
		wantCode int
	}{
		{
			name: "success returns 200",
			setup: func(agentRepo *mocks.MockAgentRepository, agentID uuid.UUID) {
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).
					Return(domainagent.Agent{ID: agentID, Role: "qa"}, nil)
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid UUID returns 400",
			id:       "not-a-uuid",
			setup:    func(agentRepo *mocks.MockAgentRepository, agentID uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "not found returns 404",
			setup: func(agentRepo *mocks.MockAgentRepository, agentID uuid.UUID) {
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).
					Return(domainagent.Agent{}, errors.New("agent not found"))
			},
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, _ := newAgentSvc(t)
			agentID := uuid.New()
			tt.setup(agentRepo, agentID)
			r := newRouter(svc)

			id := tt.id
			if id == "" {
				id = agentID.String()
			}

			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/agents/"+id, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}
