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

func newAgentSvc(t *testing.T) (*agentsvc.Service, *mocks.MockAgentRepository, *mocks.MockTaskRepository, *mocks.MockEventBus) {
	t.Helper()
	ctrl := gomock.NewController(t)
	agentRepo := mocks.NewMockAgentRepository(ctrl)
	taskRepo := mocks.NewMockTaskRepository(ctrl)
	bus := mocks.NewMockEventBus(ctrl)
	svc := agentsvc.NewService(agentRepo, taskRepo, bus)
	return svc, agentRepo, taskRepo, bus
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	svc, agentRepo, _, bus := newAgentSvc(t)
	ctx := context.Background()
	projectID := uuid.New()

	expected := domainagent.Agent{
		ID:        uuid.New(),
		ProjectID: projectID,
		Role:      "coder",
		Name:      "bot",
		Status:    domainagent.StatusIdle,
	}
	agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(expected, nil)
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	got, err := svc.Register(ctx, projectID, "coder", "bot", "gpt4", []string{})
	require.NoError(t, err)
	assert.Equal(t, expected.ID, got.ID)
	assert.Equal(t, domainagent.StatusIdle, got.Status)
}

func TestRegister_RepoError(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)

	agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domainagent.Agent{}, errors.New("db error"))

	_, err := svc.Register(context.Background(), uuid.New(), "coder", "bot", "gpt4", []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register agent")
}

// ── Reactivate ────────────────────────────────────────────────────────────────

func TestReactivate_Success(t *testing.T) {
	svc, agentRepo, _, bus := newAgentSvc(t)
	ctx := context.Background()
	agentID := uuid.New()

	stored := domainagent.Agent{ID: agentID, Status: domainagent.StatusOffline}
	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
	agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusIdle).Return(nil)
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	got, err := svc.Reactivate(ctx, agentID)
	require.NoError(t, err)
	assert.Equal(t, domainagent.StatusIdle, got.Status)
}

func TestReactivate_NotFound(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()

	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{}, errors.New("not found"))

	_, err := svc.Reactivate(context.Background(), agentID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent not found")
}

func TestReactivate_AlreadyIdle(t *testing.T) {
	// SQL no-op — agent is already idle; TypeAgentOnline still published.
	svc, agentRepo, _, bus := newAgentSvc(t)
	agentID := uuid.New()

	stored := domainagent.Agent{ID: agentID, Status: domainagent.StatusIdle}
	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
	agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusIdle).Return(nil)
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	got, err := svc.Reactivate(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, domainagent.StatusIdle, got.Status)
}

func TestReactivate_AlreadyWorking(t *testing.T) {
	// Known side effect: overwrites working → idle. V2 concern — documented here.
	svc, agentRepo, _, bus := newAgentSvc(t)
	agentID := uuid.New()

	stored := domainagent.Agent{ID: agentID, Status: domainagent.StatusWorking}
	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
	agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusIdle).Return(nil)
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	got, err := svc.Reactivate(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, domainagent.StatusIdle, got.Status)
}

// ── GetByID / List ────────────────────────────────────────────────────────────

func TestGetByID_Success(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()
	expected := domainagent.Agent{ID: agentID, Role: "qa"}

	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(expected, nil)

	got, err := svc.GetByID(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, expected.ID, got.ID)
}

func TestGetByID_NotFound(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()

	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{}, errors.New("agent not found"))

	_, err := svc.GetByID(context.Background(), agentID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get agent")
}

func TestList_Success(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	expected := []domainagent.Agent{{ID: uuid.New()}, {ID: uuid.New()}}

	agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(expected, nil)

	got, err := svc.List(context.Background(), domainagent.ListFilters{})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestList_Error(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)

	agentRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))

	_, err := svc.List(context.Background(), domainagent.ListFilters{})
	require.Error(t, err)
}

// ── SetWorking / SetIdle ──────────────────────────────────────────────────────

func TestSetWorking_Success(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()
	taskID := uuid.New()

	agentRepo.EXPECT().SetWorkingStatus(gomock.Any(), agentID, taskID).Return(nil)

	svc.SetWorking(context.Background(), agentID, taskID)
}

func TestSetWorking_OnOfflineAgent(t *testing.T) {
	// Known ordering anomaly: delayed claim_task after ReapOrphaned sets working on offline agent.
	// SQL succeeds — no guard. V2 concern.
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()
	taskID := uuid.New()

	agentRepo.EXPECT().SetWorkingStatus(gomock.Any(), agentID, taskID).Return(nil)

	svc.SetWorking(context.Background(), agentID, taskID)
}

func TestSetIdle_Success(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()

	agentRepo.EXPECT().SetIdleStatus(gomock.Any(), agentID).Return(nil)

	svc.SetIdle(context.Background(), agentID)
}

func TestSetIdle_OnOfflineAgent(t *testing.T) {
	// Symmetric to SetWorking_OnOfflineAgent: agent may be offline when SetIdle called.
	// SQL succeeds — no guard. V2 concern.
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()

	agentRepo.EXPECT().SetIdleStatus(gomock.Any(), agentID).Return(nil)

	svc.SetIdle(context.Background(), agentID)
}

// ── ListOfflineWithInflightTasks ──────────────────────────────────────────────

func TestListOfflineWithInflightTasks_Success(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	expected := []uuid.UUID{uuid.New(), uuid.New()}

	agentRepo.EXPECT().ListOfflineWithInflightTasks(gomock.Any()).Return(expected, nil)

	got, err := svc.ListOfflineWithInflightTasks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestListOfflineWithInflightTasks_Error(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)

	agentRepo.EXPECT().ListOfflineWithInflightTasks(gomock.Any()).Return(nil, errors.New("db error"))

	_, err := svc.ListOfflineWithInflightTasks(context.Background())
	require.Error(t, err)
}

// ── ReapOrphaned ──────────────────────────────────────────────────────────────

func TestReapOrphaned_NoInFlightTask(t *testing.T) {
	svc, agentRepo, taskRepo, bus := newAgentSvc(t)
	agentID := uuid.New()

	agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusOffline).Return(nil)
	taskRepo.EXPECT().UnassignByAgent(gomock.Any(), agentID).Return(nil)
	bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeAgentOffline)).Return(nil)

	svc.ReapOrphaned(context.Background(), agentID)
}

func TestReapOrphaned_AgentNotFound(t *testing.T) {
	// UpdateStatus error: function returns early without calling UnassignByAgent or Publish.
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()

	agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusOffline).Return(errors.New("agent not found"))

	svc.ReapOrphaned(context.Background(), agentID) // must not panic
}

func TestReapOrphaned_Idempotent(t *testing.T) {
	// Two calls: both UpdateStatus(offline) + UnassignByAgent are no-ops on second call.
	svc, agentRepo, taskRepo, bus := newAgentSvc(t)
	agentID := uuid.New()

	agentRepo.EXPECT().UpdateStatus(gomock.Any(), agentID, domainagent.StatusOffline).Return(nil).Times(2)
	taskRepo.EXPECT().UnassignByAgent(gomock.Any(), agentID).Return(nil).Times(2)
	bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeAgentOffline)).Return(nil).Times(2)

	svc.ReapOrphaned(context.Background(), agentID)
	svc.ReapOrphaned(context.Background(), agentID)
}

// ── ReleaseAgent ──────────────────────────────────────────────────────────────

func TestReleaseAgent_StillOffline(t *testing.T) {
	svc, agentRepo, taskRepo, _ := newAgentSvc(t)
	agentID := uuid.New()
	projectID := uuid.New()

	stored := domainagent.Agent{ID: agentID, ProjectID: projectID, Status: domainagent.StatusOffline}
	freed := []domaintask.Status{domaintask.StatusInProgress}

	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
	taskRepo.EXPECT().ReleaseInFlightByAgent(gomock.Any(), agentID).Return(freed, nil)

	gotProject, gotStatuses, err := svc.ReleaseAgent(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, projectID, gotProject)
	assert.Equal(t, freed, gotStatuses)
}

func TestReleaseAgent_AgentReconnected(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()

	// Agent reconnected during grace period.
	stored := domainagent.Agent{ID: agentID, Status: domainagent.StatusIdle}
	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)

	gotProject, gotStatuses, err := svc.ReleaseAgent(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, gotProject)
	assert.Nil(t, gotStatuses)
}

func TestReleaseAgent_NotFound(t *testing.T) {
	svc, agentRepo, _, _ := newAgentSvc(t)
	agentID := uuid.New()

	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{}, errors.New("not found"))

	_, _, err := svc.ReleaseAgent(context.Background(), agentID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent not found")
}

func TestReleaseAgent_MultipleStatuses(t *testing.T) {
	svc, agentRepo, taskRepo, _ := newAgentSvc(t)
	agentID := uuid.New()
	projectID := uuid.New()

	stored := domainagent.Agent{ID: agentID, ProjectID: projectID, Status: domainagent.StatusOffline}
	freed := []domaintask.Status{domaintask.StatusInProgress, domaintask.StatusInQA}

	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
	taskRepo.EXPECT().ReleaseInFlightByAgent(gomock.Any(), agentID).Return(freed, nil)

	gotProject, gotStatuses, err := svc.ReleaseAgent(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, projectID, gotProject)
	assert.Equal(t, freed, gotStatuses)
}

func TestReleaseAgent_ReleaseInFlightError(t *testing.T) {
	svc, agentRepo, taskRepo, _ := newAgentSvc(t)
	agentID := uuid.New()
	projectID := uuid.New()

	stored := domainagent.Agent{ID: agentID, ProjectID: projectID, Status: domainagent.StatusOffline}
	agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(stored, nil)
	taskRepo.EXPECT().ReleaseInFlightByAgent(gomock.Any(), agentID).Return(nil, errors.New("db error"))

	// The implementation returns (projectID, nil, err) — projectID is always set when agent is found.
	gotProject, gotStatuses, err := svc.ReleaseAgent(context.Background(), agentID)
	require.Error(t, err)
	assert.Equal(t, projectID, gotProject)
	assert.Nil(t, gotStatuses)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func matchEventType(et event.Type) gomock.Matcher {
	return eventTypeMatcher{et}
}

type eventTypeMatcher struct{ want event.Type }

func (m eventTypeMatcher) Matches(x interface{}) bool {
	e, ok := x.(event.Event)
	return ok && e.Type == m.want
}
func (m eventTypeMatcher) String() string { return "event.Type=" + string(m.want) }
