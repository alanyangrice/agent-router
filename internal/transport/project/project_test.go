package project_test

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

	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	"github.com/alanyang/agent-mesh/internal/mocks"
	projectsvc "github.com/alanyang/agent-mesh/internal/service/project"
	transportproject "github.com/alanyang/agent-mesh/internal/transport/project"
)

func init() { gin.SetMode(gin.TestMode) }

func newRouter(svc *projectsvc.Service) *gin.Engine {
	r := gin.New()
	transportproject.Register(r.Group("/projects"), svc)
	return r
}

func newProjectSvc(t *testing.T) (*projectsvc.Service, *mocks.MockProjectRepository) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mocks.NewMockProjectRepository(ctrl)
	return projectsvc.NewService(repo), repo
}

// ── POST / (createProject) ────────────────────────────────────────────────────

func TestCreateProject(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]string
		setup    func(repo *mocks.MockProjectRepository)
		wantCode int
		wantID   bool
	}{
		{
			name: "success returns 201 with project",
			body: map[string]string{"name": "proj", "repo_url": "https://github.com/foo"},
			setup: func(repo *mocks.MockProjectRepository) {
				created := domainproject.Project{ID: uuid.New(), Name: "proj", RepoURL: "https://github.com/foo"}
				repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(created, nil)
			},
			wantCode: http.StatusCreated,
			wantID:   true,
		},
		{
			name:     "missing required fields returns 400",
			body:     map[string]string{},
			setup:    func(repo *mocks.MockProjectRepository) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "service error returns 500",
			body: map[string]string{"name": "proj", "repo_url": "https://github.com/foo"},
			setup: func(repo *mocks.MockProjectRepository) {
				repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domainproject.Project{}, errors.New("db error"))
			},
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newProjectSvc(t)
			tt.setup(repo)
			r := newRouter(svc)

			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/projects/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			if tt.wantID {
				var got domainproject.Project
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
				assert.NotEqual(t, uuid.Nil, got.ID)
			}
		})
	}
}

// ── GET /:id (getProject) ─────────────────────────────────────────────────────

func TestGetProject(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		setup    func(repo *mocks.MockProjectRepository, id uuid.UUID)
		wantCode int
	}{
		{
			name: "success returns 200",
			setup: func(repo *mocks.MockProjectRepository, id uuid.UUID) {
				repo.EXPECT().GetByID(gomock.Any(), id).Return(domainproject.Project{ID: id, Name: "proj"}, nil)
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "invalid UUID returns 400",
			id:       "not-a-uuid",
			setup:    func(repo *mocks.MockProjectRepository, id uuid.UUID) {},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "not found returns 404",
			setup: func(repo *mocks.MockProjectRepository, id uuid.UUID) {
				repo.EXPECT().GetByID(gomock.Any(), id).Return(domainproject.Project{}, errors.New("not found"))
			},
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newProjectSvc(t)
			projectID := uuid.New()
			tt.setup(repo, projectID)
			r := newRouter(svc)

			id := tt.id
			if id == "" {
				id = projectID.String()
			}

			w := httptest.NewRecorder()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+id, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
		})
	}
}
