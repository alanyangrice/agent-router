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

func TestCreateProject_Success(t *testing.T) {
	svc, repo := newProjectSvc(t)
	r := newRouter(svc)

	created := domainproject.Project{ID: uuid.New(), Name: "proj", RepoURL: "https://github.com/foo"}
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(created, nil)

	body, _ := json.Marshal(map[string]string{"name": "proj", "repo_url": "https://github.com/foo"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var got domainproject.Project
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, created.ID, got.ID)
}

func TestCreateProject_BadBody(t *testing.T) {
	svc, _ := newProjectSvc(t)
	r := newRouter(svc)

	// Missing required fields.
	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateProject_ServiceError(t *testing.T) {
	svc, repo := newProjectSvc(t)
	r := newRouter(svc)

	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domainproject.Project{}, errors.New("db error"))

	body, _ := json.Marshal(map[string]string{"name": "proj", "repo_url": "https://github.com/foo"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/projects/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ── GET /:id (getProject) ────────────────────────────────────────────────────

func TestGetProject_Success(t *testing.T) {
	svc, repo := newProjectSvc(t)
	r := newRouter(svc)
	projectID := uuid.New()

	repo.EXPECT().GetByID(gomock.Any(), projectID).Return(domainproject.Project{ID: projectID, Name: "proj"}, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+projectID.String(), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var got domainproject.Project
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, projectID, got.ID)
}

func TestGetProject_InvalidID(t *testing.T) {
	svc, _ := newProjectSvc(t)
	r := newRouter(svc)

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/not-a-uuid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetProject_NotFound(t *testing.T) {
	svc, repo := newProjectSvc(t)
	r := newRouter(svc)
	projectID := uuid.New()

	repo.EXPECT().GetByID(gomock.Any(), projectID).Return(domainproject.Project{}, errors.New("not found"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+projectID.String(), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
