package mcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	mcptransport "github.com/alanyang/agent-mesh/internal/transport/mcp"
)

// ── Registry ──────────────────────────────────────────────────────────────────

func TestRegistry(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "register then unregister removes agent",
			run: func(t *testing.T) {
				reg := mcptransport.NewSessionRegistry()
				agentID := uuid.New()
				projectID := uuid.New()

				reg.Register("session-1", agentID, projectID, "coder")
				assert.True(t, reg.IsConnected(agentID), "agent should be connected after register")

				got, ok := reg.Unregister("session-1")
				assert.True(t, ok, "unregister should succeed")
				assert.Equal(t, agentID, got)
				assert.False(t, reg.IsConnected(agentID), "agent should not be connected after unregister")
			},
		},
		{
			name: "re-registering agent with new session replaces old session",
			run: func(t *testing.T) {
				reg := mcptransport.NewSessionRegistry()
				agentID := uuid.New()
				projectID := uuid.New()

				reg.Register("session-old", agentID, projectID, "coder")
				reg.Register("session-new", agentID, projectID, "coder")

				// Old session is no longer valid.
				got, ok := reg.Unregister("session-old")
				assert.False(t, ok, "old session should not exist after re-register")
				assert.Equal(t, uuid.Nil, got)

				// New session still active.
				assert.True(t, reg.IsConnected(agentID))
			},
		},
		{
			name: "unregister non-existent session returns nil, false",
			run: func(t *testing.T) {
				reg := mcptransport.NewSessionRegistry()
				got, ok := reg.Unregister("does-not-exist")
				assert.False(t, ok)
				assert.Equal(t, uuid.Nil, got)
			},
		},
		{
			name: "session close unregisters agent — orphan recovery simulation",
			run: func(t *testing.T) {
				reg := mcptransport.NewSessionRegistry()
				agentID := uuid.New()
				reg.Register("session-drop", agentID, uuid.New(), "coder")
				assert.True(t, reg.IsConnected(agentID))

				recovered, ok := reg.Unregister("session-drop")
				assert.True(t, ok)
				assert.Equal(t, agentID, recovered)
				assert.False(t, reg.IsConnected(agentID))
			},
		},
		{
			name: "agent can reconnect after disconnect",
			run: func(t *testing.T) {
				reg := mcptransport.NewSessionRegistry()
				agentID := uuid.New()
				projectID := uuid.New()

				reg.Register("session-1", agentID, projectID, "coder")
				assert.True(t, reg.IsConnected(agentID))

				reg.Unregister("session-1")
				assert.False(t, reg.IsConnected(agentID))

				reg.Register("session-2", agentID, projectID, "coder")
				assert.True(t, reg.IsConnected(agentID), "agent should be connected after reconnect")
			},
		},
		{
			name: "NotifyAgent for disconnected agent is a no-op",
			run: func(t *testing.T) {
				reg := mcptransport.NewSessionRegistry()
				err := reg.NotifyAgent(context.Background(), uuid.New(), map[string]string{"event": "test"})
				assert.NoError(t, err)
			},
		},
		{
			name: "NotifyProjectRole with no sessions is a no-op",
			run: func(t *testing.T) {
				reg := mcptransport.NewSessionRegistry()
				err := reg.NotifyProjectRole(context.Background(), uuid.New(), "coder", map[string]string{"event": "main_updated"})
				assert.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}

// ── Pipeline (already table-driven — preserved as-is) ─────────────────────────

func TestPipeline_DefaultConfig_Transitions(t *testing.T) {
	cfg := pipeline.DefaultConfig

	tests := []struct {
		status              domaintask.Status
		expectAssignRole    string
		expectFreedRole     string
		expectBroadcast     string
		expectBroadcastRole string
	}{
		{domaintask.StatusReady, "coder", "", "", ""},
		{domaintask.StatusInProgress, "", "coder", "", ""},
		{domaintask.StatusInQA, "qa", "qa", "", ""},
		{domaintask.StatusInReview, "reviewer", "reviewer", "", ""},
		{domaintask.StatusMerged, "", "", "main_updated", "coder"},
	}

	for _, tc := range tests {
		action, ok := cfg[tc.status]
		require.True(t, ok, "status %s should have a pipeline action", tc.status)
		assert.Equal(t, tc.expectAssignRole, action.AssignRole, "status %s AssignRole", tc.status)
		assert.Equal(t, tc.expectFreedRole, action.FreedRole, "status %s FreedRole", tc.status)
		assert.Equal(t, tc.expectBroadcast, action.BroadcastEvent, "status %s BroadcastEvent", tc.status)
		assert.Equal(t, tc.expectBroadcastRole, action.BroadcastRole, "status %s BroadcastRole", tc.status)
	}
}

func TestPipeline_ValidTransitions(t *testing.T) {
	tests := []struct {
		from    domaintask.Status
		to      domaintask.Status
		allowed bool
	}{
		{domaintask.StatusBacklog, domaintask.StatusReady, true},
		{domaintask.StatusReady, domaintask.StatusInProgress, true},
		{domaintask.StatusInProgress, domaintask.StatusInQA, true},
		{domaintask.StatusInQA, domaintask.StatusInReview, true},
		{domaintask.StatusInQA, domaintask.StatusInProgress, true},  // QA fail
		{domaintask.StatusInReview, domaintask.StatusMerged, true},
		{domaintask.StatusInReview, domaintask.StatusInProgress, true}, // reviewer reject
		{domaintask.StatusMerged, domaintask.StatusInProgress, false},  // terminal
		{domaintask.StatusInProgress, domaintask.StatusMerged, false},  // must go through QA/review
	}

	for _, tc := range tests {
		got := tc.from.CanTransitionTo(tc.to)
		assert.Equal(t, tc.allowed, got, "%s → %s", tc.from, tc.to)
	}
}

// ── Skipped integration tests (require running server) ────────────────────────

// TestHappyPath exercises the full coder → QA → reviewer pipeline via REST.
// Run via: scripts/test_v1_rest.sh
func TestHappyPath(t *testing.T) {
	t.Skip("Requires running server — run via scripts/test_v1_rest.sh")
}

// TestOwnershipLock verifies assigned_agent_id is preserved when QA bounces back.
// Run via: agents/example/simulate_pipeline.py
func TestOwnershipLock(t *testing.T) {
	t.Skip("Requires running server — run via agents/example/simulate_pipeline.py")
}

// TestReviewerBounce verifies assigned_agent_id is preserved when reviewer bounces back.
func TestReviewerBounce(t *testing.T) {
	t.Skip("Requires running server — run via agents/example/simulate_pipeline.py")
}

// TestMainUpdatedNotification verifies merged task triggers broadcast to in-progress coders.
func TestMainUpdatedNotification(t *testing.T) {
	t.Skip("Requires running server — run via agents/example/simulate_pipeline.py")
}

// TestPromptFallback verifies global default is returned when no project-specific prompt exists.
func TestPromptFallback(t *testing.T) {
	t.Skip("Requires running Postgres — run via scripts/test_v1_rest.sh")
}
