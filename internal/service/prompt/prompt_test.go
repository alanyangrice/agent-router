package prompt_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domainprompt "github.com/alanyang/agent-mesh/internal/domain/prompt"
	"github.com/alanyang/agent-mesh/internal/mocks"
	promptsvc "github.com/alanyang/agent-mesh/internal/service/prompt"
)

func newPromptSvc(t *testing.T) (*promptsvc.Service, *mocks.MockPromptRepository) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mocks.NewMockPromptRepository(ctrl)
	return promptsvc.NewService(repo), repo
}

func TestGetForRole(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo *mocks.MockPromptRepository, projectID uuid.UUID)
		wantErr bool
		wantMsg string
	}{
		{
			name: "success",
			setup: func(repo *mocks.MockPromptRepository, projectID uuid.UUID) {
				repo.EXPECT().GetForRole(gomock.Any(), &projectID, "coder").
					Return(domainprompt.RolePrompt{Role: "coder", Content: "You are a coder"}, nil)
			},
		},
		{
			name: "repo error",
			setup: func(repo *mocks.MockPromptRepository, projectID uuid.UUID) {
				repo.EXPECT().GetForRole(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(domainprompt.RolePrompt{}, errors.New("db error"))
			},
			wantErr: true,
			wantMsg: "get role prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newPromptSvc(t)
			projectID := uuid.New()
			tt.setup(repo, projectID)

			got, err := svc.GetForRole(context.Background(), projectID, "coder")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "coder", got.Role)
		})
	}
}

func TestSet(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo *mocks.MockPromptRepository)
		wantErr bool
		wantMsg string
	}{
		{
			name: "success",
			setup: func(repo *mocks.MockPromptRepository) {
				repo.EXPECT().Set(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "repo error",
			setup: func(repo *mocks.MockPromptRepository) {
				repo.EXPECT().Set(gomock.Any(), gomock.Any()).Return(errors.New("db error"))
			},
			wantErr: true,
			wantMsg: "set role prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newPromptSvc(t)
			tt.setup(repo)

			err := svc.Set(context.Background(), uuid.New(), "coder", "You are a coder")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestList(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(repo *mocks.MockPromptRepository, projectID uuid.UUID)
		wantLen  int
		wantErr  bool
		wantMsg  string
	}{
		{
			name: "success returns all prompts",
			setup: func(repo *mocks.MockPromptRepository, projectID uuid.UUID) {
				repo.EXPECT().List(gomock.Any(), projectID).
					Return([]domainprompt.RolePrompt{{Role: "coder"}, {Role: "qa"}}, nil)
			},
			wantLen: 2,
		},
		{
			name: "repo error",
			setup: func(repo *mocks.MockPromptRepository, projectID uuid.UUID) {
				repo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))
			},
			wantErr: true,
			wantMsg: "list role prompts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newPromptSvc(t)
			projectID := uuid.New()
			tt.setup(repo, projectID)

			got, err := svc.List(context.Background(), projectID)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}
