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

func TestCreate(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo *mocks.MockProjectRepository) domainproject.Project
		wantErr bool
		wantMsg string
	}{
		{
			name: "success",
			setup: func(repo *mocks.MockProjectRepository) domainproject.Project {
				expected := domainproject.Project{ID: uuid.New(), Name: "my-project", RepoURL: "https://github.com/foo"}
				repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(expected, nil)
				return expected
			},
		},
		{
			name: "repo error",
			setup: func(repo *mocks.MockProjectRepository) domainproject.Project {
				repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domainproject.Project{}, errors.New("db error"))
				return domainproject.Project{}
			},
			wantErr: true,
			wantMsg: "create project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newProjectSvc(t)
			expected := tt.setup(repo)

			got, err := svc.Create(context.Background(), "my-project", "https://github.com/foo")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, expected.ID, got.ID)
			assert.Equal(t, expected.Name, got.Name)
		})
	}
}

func TestGetByID(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo *mocks.MockProjectRepository, id uuid.UUID)
		wantErr bool
		wantMsg string
	}{
		{
			name: "success",
			setup: func(repo *mocks.MockProjectRepository, id uuid.UUID) {
				repo.EXPECT().GetByID(gomock.Any(), id).Return(domainproject.Project{ID: id, Name: "proj"}, nil)
			},
		},
		{
			name: "not found",
			setup: func(repo *mocks.MockProjectRepository, id uuid.UUID) {
				repo.EXPECT().GetByID(gomock.Any(), id).Return(domainproject.Project{}, errors.New("not found"))
			},
			wantErr: true,
			wantMsg: "get project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newProjectSvc(t)
			projectID := uuid.New()
			tt.setup(repo, projectID)

			got, err := svc.GetByID(context.Background(), projectID)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, projectID, got.ID)
		})
	}
}
