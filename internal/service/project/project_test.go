package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	"github.com/alanyang/agent-mesh/internal/mocks"
	projectsvc "github.com/alanyang/agent-mesh/internal/service/project"
)

func newProjectSvc(t *testing.T) (*projectsvc.Service, *mocks.MockProjectRepository) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mocks.NewMockProjectRepository(ctrl)
	return projectsvc.NewService(repo), repo
}

func TestCreate_Success(t *testing.T) {
	svc, repo := newProjectSvc(t)
	expected := domainproject.Project{ID: uuid.New(), Name: "my-project", RepoURL: "https://github.com/foo"}
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(expected, nil)

	got, err := svc.Create(context.Background(), "my-project", "https://github.com/foo")
	require.NoError(t, err)
	assert.Equal(t, expected.ID, got.ID)
	assert.Equal(t, "my-project", got.Name)
}

func TestCreate_RepoError(t *testing.T) {
	svc, repo := newProjectSvc(t)
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domainproject.Project{}, errors.New("db error"))

	_, err := svc.Create(context.Background(), "x", "y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create project")
}

func TestGetByID_Success(t *testing.T) {
	svc, repo := newProjectSvc(t)
	projectID := uuid.New()
	expected := domainproject.Project{ID: projectID, Name: "proj"}
	repo.EXPECT().GetByID(gomock.Any(), projectID).Return(expected, nil)

	got, err := svc.GetByID(context.Background(), projectID)
	require.NoError(t, err)
	assert.Equal(t, projectID, got.ID)
}

func TestGetByID_NotFound(t *testing.T) {
	svc, repo := newProjectSvc(t)
	projectID := uuid.New()
	repo.EXPECT().GetByID(gomock.Any(), projectID).Return(domainproject.Project{}, errors.New("not found"))

	_, err := svc.GetByID(context.Background(), projectID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get project")
}
