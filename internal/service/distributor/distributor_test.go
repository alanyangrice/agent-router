package distributor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/alanyang/agent-mesh/internal/mocks"
	"github.com/alanyang/agent-mesh/internal/service/distributor"
)

func newDistributorSvc(t *testing.T) (*distributor.Service, *mocks.MockAgentRepository) {
	t.Helper()
	ctrl := gomock.NewController(t)
	agentRepo := mocks.NewMockAgentRepository(ctrl)
	return distributor.NewService(agentRepo), agentRepo
}

func TestDistribute_Success(t *testing.T) {
	svc, repo := newDistributorSvc(t)
	projectID := uuid.New()
	agentID := uuid.New()

	repo.EXPECT().ClaimAgent(gomock.Any(), projectID, "coder").Return(agentID, nil)

	got, err := svc.Distribute(context.Background(), projectID, "coder")
	require.NoError(t, err)
	assert.Equal(t, agentID, got)
}

func TestDistribute_NoAgents(t *testing.T) {
	// ClaimAgent returns ErrNoAgentAvailable when no idle agent found.
	svc, repo := newDistributorSvc(t)
	projectID := uuid.New()

	repo.EXPECT().ClaimAgent(gomock.Any(), projectID, "coder").Return(uuid.Nil, distributor.ErrNoAgentAvailable)

	_, err := svc.Distribute(context.Background(), projectID, "coder")
	require.Error(t, err)
	assert.True(t, errors.Is(err, distributor.ErrNoAgentAvailable))
}

func TestDistribute_RepoError(t *testing.T) {
	svc, repo := newDistributorSvc(t)

	repo.EXPECT().ClaimAgent(gomock.Any(), gomock.Any(), gomock.Any()).Return(uuid.Nil, errors.New("unexpected db error"))

	_, err := svc.Distribute(context.Background(), uuid.New(), "qa")
	require.Error(t, err)
}

func TestDistribute_SkipsWorkingAgents(t *testing.T) {
	// Working-agent exclusion is enforced in ClaimAgent SQL (WHERE status='idle').
	// This test documents that behavior: ClaimAgent returns no-agent when all are working.
	svc, repo := newDistributorSvc(t)
	projectID := uuid.New()

	// All agents are working → ClaimAgent returns ErrNoAgentAvailable.
	repo.EXPECT().ClaimAgent(gomock.Any(), projectID, "qa").Return(uuid.Nil, distributor.ErrNoAgentAvailable)

	_, err := svc.Distribute(context.Background(), projectID, "qa")
	require.Error(t, err, "distributor must return error when all agents are working")
}
