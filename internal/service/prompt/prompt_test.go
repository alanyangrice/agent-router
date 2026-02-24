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

func TestGetForRole_Success(t *testing.T) {
	svc, repo := newPromptSvc(t)
	projectID := uuid.New()
	expected := domainprompt.RolePrompt{Role: "coder", Content: "You are a coder"}
	repo.EXPECT().GetForRole(gomock.Any(), &projectID, "coder").Return(expected, nil)

	got, err := svc.GetForRole(context.Background(), projectID, "coder")
	require.NoError(t, err)
	assert.Equal(t, "coder", got.Role)
}

func TestGetForRole_Error(t *testing.T) {
	svc, repo := newPromptSvc(t)
	repo.EXPECT().GetForRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(domainprompt.RolePrompt{}, errors.New("db error"))

	_, err := svc.GetForRole(context.Background(), uuid.New(), "coder")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get role prompt")
}

func TestSet_Success(t *testing.T) {
	svc, repo := newPromptSvc(t)
	projectID := uuid.New()
	repo.EXPECT().Set(gomock.Any(), gomock.Any()).Return(nil)

	err := svc.Set(context.Background(), projectID, "coder", "You are a coder")
	require.NoError(t, err)
}

func TestSet_Error(t *testing.T) {
	svc, repo := newPromptSvc(t)
	repo.EXPECT().Set(gomock.Any(), gomock.Any()).Return(errors.New("db error"))

	err := svc.Set(context.Background(), uuid.New(), "coder", "content")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "set role prompt")
}

func TestList_Success(t *testing.T) {
	svc, repo := newPromptSvc(t)
	projectID := uuid.New()
	expected := []domainprompt.RolePrompt{{Role: "coder"}, {Role: "qa"}}
	repo.EXPECT().List(gomock.Any(), projectID).Return(expected, nil)

	got, err := svc.List(context.Background(), projectID)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestList_Error(t *testing.T) {
	svc, repo := newPromptSvc(t)
	repo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))

	_, err := svc.List(context.Background(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list role prompts")
}
