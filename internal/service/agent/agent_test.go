package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	"github.com/alanyang/agent-mesh/internal/domain/event"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	"github.com/alanyang/agent-mesh/internal/mocks"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newAgentSvc(t *testing.T) (*agentsvc.Service, *mocks.MockAgentRepository, *mocks.MockTaskRepository, *mocks.MockEventBus) {
	t.Helper()
	ctrl := gomock.NewController(t)
	agentRepo := mocks.NewMockAgentRepository(ctrl)
	taskRepo := mocks.NewMockTaskRepository(ctrl)
	bus := mocks.NewMockEventBus(ctrl)
	svc := agentsvc.NewService(agentRepo, taskRepo, bus)
	return svc, agentRepo, taskRepo, bus
}

func matchEventType(et event.Type) gomock.Matcher {
	return eventTypeMatcher{et}
}

type eventTypeMatcher struct{ want event.Type }

func (m eventTypeMatcher) Matches(x interface{}) bool {
	e, ok := x.(event.Event)
	return ok && e.Type == m.want
}
func (m eventTypeMatcher) String() string { return "event.Type=" + string(m.want) }

// ── Register ──────────────────────────────────────────────────────────────────

func TestRegister(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(agentRepo *mocks.MockAgentRepository, bus *mocks.MockEventBus, projectID uuid.UUID) domainagent.Agent
		wantErr bool
		wantMsg string
	}{
		{
			name: "success creates idle agent",
			setup: func(agentRepo *mocks.MockAgentRepository, bus *mocks.MockEventBus, projectID uuid.UUID) domainagent.Agent {
				expected := domainagent.Agent{ID: uuid.New(), ProjectID: projectID, Role: "coder", Status: domainagent.StatusIdle}
				agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(expected, nil)
				bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
				return expected
			},
		},
		{
			name: "repo error",
			setup: func(agentRepo *mocks.MockAgentRepository, bus *mocks.MockEventBus, projectID uuid.UUID) domainagent.Agent {
				agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domainagent.Agent{}, errors.New("db error"))
				return domainagent.Agent{}
			},
			wantErr: true,
			wantMsg: "register agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, bus := newAgentSvc(t)
			projectID := uuid.New()
			expected := tt.setup(agentRepo, bus, projectID)

			got, err := svc.Register(context.Background(), projectID, "coder", "bot", "gpt4", []string{})
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, expected.ID, got.ID)
			assert.Equal(t, domainagent.StatusIdle, got.Status)
		})
	}
}

// ── Reactivate ────────────────────────────────────────────────────────────────

func TestReactivate(t *testing.T) {
	tests := []struct {
		name         string
		initialStatus domainagent.Status
		setup        func(agentRepo *mocks.MockAgentRepository, bus *mocks.MockEventBus, agentID uuid.UUID)
		wantErr      bool
		wantMsg      string
	}{
		{
			name:          "offline agent reactivated to idle",
			initialStatus: domainagent.StatusOffline,
			setup: func(agentRepo *mocks.MockAgentRepository, bus *mocks.MockEventBus, agentID uuid.UUID) {
				stored := domainagent.Agent{ID: agentID, Status: domainagent.StatusOffline}
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
				agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusIdle).Return(nil)
				bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			// SQL is a no-op but TypeAgentOnline is still published.
			name:          "already idle — still publishes online event",
			initialStatus: domainagent.StatusIdle,
			setup: func(agentRepo *mocks.MockAgentRepository, bus *mocks.MockEventBus, agentID uuid.UUID) {
				stored := domainagent.Agent{ID: agentID, Status: domainagent.StatusIdle}
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
				agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusIdle).Return(nil)
				bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			// Known side effect: overwrites working → idle. V2 concern.
			name:          "working agent overwritten to idle (V2 concern)",
			initialStatus: domainagent.StatusWorking,
			setup: func(agentRepo *mocks.MockAgentRepository, bus *mocks.MockEventBus, agentID uuid.UUID) {
				stored := domainagent.Agent{ID: agentID, Status: domainagent.StatusWorking}
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
				agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusIdle).Return(nil)
				bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "agent not found returns error",
			setup: func(agentRepo *mocks.MockAgentRepository, bus *mocks.MockEventBus, agentID uuid.UUID) {
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{}, errors.New("not found"))
			},
			wantErr: true,
			wantMsg: "agent not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, bus := newAgentSvc(t)
			agentID := uuid.New()
			tt.setup(agentRepo, bus, agentID)

			got, err := svc.Reactivate(context.Background(), agentID)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, domainagent.StatusIdle, got.Status)
		})
	}
}

// ── GetByID ───────────────────────────────────────────────────────────────────

func TestGetByID(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(agentRepo *mocks.MockAgentRepository, agentID uuid.UUID)
		wantErr bool
		wantMsg string
	}{
		{
			name: "success",
			setup: func(agentRepo *mocks.MockAgentRepository, agentID uuid.UUID) {
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).
					Return(domainagent.Agent{ID: agentID, Role: "qa"}, nil)
			},
		},
		{
			name: "not found",
			setup: func(agentRepo *mocks.MockAgentRepository, agentID uuid.UUID) {
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).
					Return(domainagent.Agent{}, errors.New("agent not found"))
			},
			wantErr: true,
			wantMsg: "get agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, _ := newAgentSvc(t)
			agentID := uuid.New()
			tt.setup(agentRepo, agentID)

			got, err := svc.GetByID(context.Background(), agentID)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, agentID, got.ID)
		})
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestList(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(agentRepo *mocks.MockAgentRepository)
		wantLen int
		wantErr bool
	}{
		{
			name: "success returns all agents",
			setup: func(agentRepo *mocks.MockAgentRepository) {
				agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).
					Return([]domainagent.Agent{{ID: uuid.New()}, {ID: uuid.New()}}, nil)
			},
			wantLen: 2,
		},
		{
			name: "repo error",
			setup: func(agentRepo *mocks.MockAgentRepository) {
				agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, _ := newAgentSvc(t)
			tt.setup(agentRepo)

			got, err := svc.List(context.Background(), domainagent.ListFilters{})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

// ── SetWorking / SetIdle ──────────────────────────────────────────────────────

func TestSetWorking(t *testing.T) {
	tests := []struct {
		name string
		// Both rows exercise the same SQL path; the second documents a V2 ordering anomaly.
		note string
	}{
		{name: "idle agent set to working"},
		{
			name: "offline agent set to working (V2 ordering anomaly — no guard)",
			// Known: delayed claim_task after ReapOrphaned may set working on an offline agent.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, _ := newAgentSvc(t)
			agentID, taskID := uuid.New(), uuid.New()
			agentRepo.EXPECT().SetWorkingStatus(gomock.Any(), agentID, taskID).Return(nil)
			svc.SetWorking(context.Background(), agentID, taskID)
		})
	}
}

func TestSetIdle(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "working agent set to idle"},
		{
			// Symmetric to SetWorking offline anomaly: SetIdle may be called on an offline agent.
			name: "offline agent set to idle (V2 ordering anomaly — no guard)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, _ := newAgentSvc(t)
			agentID := uuid.New()
			agentRepo.EXPECT().SetIdleStatus(gomock.Any(), agentID).Return(nil)
			svc.SetIdle(context.Background(), agentID)
		})
	}
}

// ── ListOfflineWithInflightTasks ──────────────────────────────────────────────

func TestListOfflineWithInflightTasks(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(agentRepo *mocks.MockAgentRepository) []uuid.UUID
		wantErr bool
	}{
		{
			name: "success returns offline agent IDs",
			setup: func(agentRepo *mocks.MockAgentRepository) []uuid.UUID {
				expected := []uuid.UUID{uuid.New(), uuid.New()}
				agentRepo.EXPECT().ListOfflineWithInflightTasks(gomock.Any()).Return(expected, nil)
				return expected
			},
		},
		{
			name: "repo error",
			setup: func(agentRepo *mocks.MockAgentRepository) []uuid.UUID {
				agentRepo.EXPECT().ListOfflineWithInflightTasks(gomock.Any()).Return(nil, errors.New("db error"))
				return nil
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, _, _ := newAgentSvc(t)
			expected := tt.setup(agentRepo)

			got, err := svc.ListOfflineWithInflightTasks(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, expected, got)
		})
	}
}

// ── ReapOrphaned ──────────────────────────────────────────────────────────────

func TestReapOrphaned(t *testing.T) {
	tests := []struct {
		name  string
		setup func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, bus *mocks.MockEventBus, agentID uuid.UUID)
	}{
		{
			name: "marks offline, unassigns ready tasks, publishes offline event",
			setup: func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, bus *mocks.MockEventBus, agentID uuid.UUID) {
				agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusOffline).Return(nil)
				taskRepo.EXPECT().UnassignByAgent(gomock.Any(), agentID).Return(nil)
				bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeAgentOffline)).Return(nil)
			},
		},
		{
			name: "UpdateStatus error — returns early without unassign or publish",
			setup: func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, bus *mocks.MockEventBus, agentID uuid.UUID) {
				agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusOffline).
					Return(errors.New("agent not found"))
			},
		},
		{
			name: "idempotent — second call is a no-op in SQL",
			setup: func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, bus *mocks.MockEventBus, agentID uuid.UUID) {
				agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusOffline).Return(nil).Times(2)
				taskRepo.EXPECT().UnassignByAgent(gomock.Any(), agentID).Return(nil).Times(2)
				bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeAgentOffline)).Return(nil).Times(2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, taskRepo, bus := newAgentSvc(t)
			agentID := uuid.New()
			tt.setup(agentRepo, taskRepo, bus, agentID)

			if tt.name == "idempotent — second call is a no-op in SQL" {
				svc.ReapOrphaned(context.Background(), agentID)
				svc.ReapOrphaned(context.Background(), agentID)
			} else {
				svc.ReapOrphaned(context.Background(), agentID)
			}
		})
	}
}

// ── ReleaseAgent ──────────────────────────────────────────────────────────────

func TestReleaseAgent(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, agentID uuid.UUID) (uuid.UUID, []domaintask.Status)
		wantErr        bool
		wantMsg        string
		wantNilProject bool
	}{
		{
			name: "offline agent — releases in-flight tasks",
			setup: func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, agentID uuid.UUID) (uuid.UUID, []domaintask.Status) {
				projectID := uuid.New()
				freed := []domaintask.Status{domaintask.StatusInProgress}
				stored := domainagent.Agent{ID: agentID, ProjectID: projectID, Status: domainagent.StatusOffline}
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
				taskRepo.EXPECT().ReleaseInFlightByAgent(gomock.Any(), agentID).Return(freed, nil)
				return projectID, freed
			},
		},
		{
			name: "agent reconnected before grace expires — no-op",
			setup: func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, agentID uuid.UUID) (uuid.UUID, []domaintask.Status) {
				stored := domainagent.Agent{ID: agentID, Status: domainagent.StatusIdle}
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
				return uuid.Nil, nil
			},
			wantNilProject: true,
		},
		{
			name: "multiple freed statuses returned",
			setup: func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, agentID uuid.UUID) (uuid.UUID, []domaintask.Status) {
				projectID := uuid.New()
				freed := []domaintask.Status{domaintask.StatusInProgress, domaintask.StatusInQA}
				stored := domainagent.Agent{ID: agentID, ProjectID: projectID, Status: domainagent.StatusOffline}
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
				taskRepo.EXPECT().ReleaseInFlightByAgent(gomock.Any(), agentID).Return(freed, nil)
				return projectID, freed
			},
		},
		{
			name: "agent not found",
			setup: func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, agentID uuid.UUID) (uuid.UUID, []domaintask.Status) {
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{}, errors.New("not found"))
				return uuid.Nil, nil
			},
			wantErr: true,
			wantMsg: "agent not found",
		},
		{
			name: "ReleaseInFlight error propagates",
			setup: func(agentRepo *mocks.MockAgentRepository, taskRepo *mocks.MockTaskRepository, agentID uuid.UUID) (uuid.UUID, []domaintask.Status) {
				projectID := uuid.New()
				stored := domainagent.Agent{ID: agentID, ProjectID: projectID, Status: domainagent.StatusOffline}
				agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
				taskRepo.EXPECT().ReleaseInFlightByAgent(gomock.Any(), agentID).Return(nil, errors.New("db error"))
				return projectID, nil
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, agentRepo, taskRepo, _ := newAgentSvc(t)
			agentID := uuid.New()
			wantProject, wantStatuses := tt.setup(agentRepo, taskRepo, agentID)

			gotProject, gotStatuses, err := svc.ReleaseAgent(context.Background(), agentID)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantMsg != "" {
					assert.Contains(t, err.Error(), tt.wantMsg)
				}
				return
			}
			require.NoError(t, err)
			if tt.wantNilProject {
				assert.Equal(t, uuid.Nil, gotProject)
				assert.Nil(t, gotStatuses)
			} else {
				assert.Equal(t, wantProject, gotProject)
				assert.Equal(t, wantStatuses, gotStatuses)
			}
		})
	}
}
