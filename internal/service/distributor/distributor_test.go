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

func TestDistribute(t *testing.T) {
	tests := []struct {
		name         string
		role         string
		setup        func(repo *mocks.MockAgentRepository, projectID uuid.UUID) uuid.UUID
		wantErr      bool
		wantSentinel error
	}{
		{
			name: "success returns claimed agent ID",
			role: "coder",
			setup: func(repo *mocks.MockAgentRepository, projectID uuid.UUID) uuid.UUID {
				agentID := uuid.New()
				repo.EXPECT().ClaimAgent(gomock.Any(), projectID, "coder").Return(agentID, nil)
				return agentID
			},
		},
		{
			name: "no agents available returns ErrNoAgentAvailable",
			role: "coder",
			setup: func(repo *mocks.MockAgentRepository, projectID uuid.UUID) uuid.UUID {
				repo.EXPECT().ClaimAgent(gomock.Any(), projectID, "coder").
					Return(uuid.Nil, distributor.ErrNoAgentAvailable)
				return uuid.Nil
			},
			wantErr:      true,
			wantSentinel: distributor.ErrNoAgentAvailable,
		},
		{
			name: "unexpected repo error propagates",
			role: "qa",
			setup: func(repo *mocks.MockAgentRepository, projectID uuid.UUID) uuid.UUID {
				repo.EXPECT().ClaimAgent(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(uuid.Nil, errors.New("unexpected db error"))
				return uuid.Nil
			},
			wantErr: true,
		},
		{
			// Working-agent exclusion is enforced in ClaimAgent SQL (WHERE status='idle').
			// When all agents are working, ClaimAgent returns ErrNoAgentAvailable.
			name: "all agents working — ClaimAgent returns no-agent",
			role: "qa",
			setup: func(repo *mocks.MockAgentRepository, projectID uuid.UUID) uuid.UUID {
				repo.EXPECT().ClaimAgent(gomock.Any(), projectID, "qa").
					Return(uuid.Nil, distributor.ErrNoAgentAvailable)
				return uuid.Nil
			},
			wantErr:      true,
			wantSentinel: distributor.ErrNoAgentAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newDistributorSvc(t)
			projectID := uuid.New()
			wantID := tt.setup(repo, projectID)

			got, err := svc.Distribute(context.Background(), projectID, tt.role)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantSentinel != nil {
					assert.ErrorIs(t, err, tt.wantSentinel)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, wantID, got)
		})
	}
}
