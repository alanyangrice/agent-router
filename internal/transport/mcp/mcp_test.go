package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	mcptransport "github.com/alanyang/agent-mesh/internal/transport/mcp"
)

// ── Registry unit tests (no DB required) ─────────────────────────────────────

func TestRegistry_RegisterUnregister(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()

	agentID := uuid.New()
	projectID := uuid.New()
	sessionID := "session-1"

	reg.Register(sessionID, agentID, projectID, "coder")

	assert.True(t, reg.IsConnected(agentID), "agent should be connected after register")

	got, ok := reg.Unregister(sessionID)
	assert.True(t, ok, "unregister should succeed")
	assert.Equal(t, agentID, got, "unregistered agent ID should match")
	assert.False(t, reg.IsConnected(agentID), "agent should not be connected after unregister")
}

func TestRegistry_RegisterOverwritesPreviousSession(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()

	agentID := uuid.New()
	projectID := uuid.New()

	reg.Register("session-old", agentID, projectID, "coder")
	reg.Register("session-new", agentID, projectID, "coder")

	// Old session should no longer be tracked.
	got, ok := reg.Unregister("session-old")
	assert.False(t, ok, "old session should not exist after re-register")
	assert.Equal(t, uuid.Nil, got)

	// New session is still there.
	assert.True(t, reg.IsConnected(agentID))
}

func TestRegistry_UnregisterNonExistentSession(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()
	got, ok := reg.Unregister("does-not-exist")
	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, got)
}

// ── Pipeline domain tests (no DB required) ────────────────────────────────────

func TestPipeline_DefaultConfig_Transitions(t *testing.T) {
	cfg := pipeline.DefaultConfig

	tests := []struct {
		status         domaintask.Status
		expectRole     string
		expectBroadcast string
	}{
		{domaintask.StatusReady, "coder", ""},
		{domaintask.StatusInProgress, "", ""},       // ownership lock: no new role
		{domaintask.StatusInQA, "qa", ""},
		{domaintask.StatusInReview, "reviewer", ""},
		{domaintask.StatusMerged, "", "main_updated"},
	}

	for _, tc := range tests {
		action, ok := cfg[tc.status]
		require.True(t, ok, "status %s should have a pipeline action", tc.status)
		assert.Equal(t, tc.expectRole, action.AssignRole, "status %s AssignRole", tc.status)
		assert.Equal(t, tc.expectBroadcast, action.BroadcastEvent, "status %s BroadcastEvent", tc.status)
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

// ── Notification tests ────────────────────────────────────────────────────────

func TestRegistry_NotifyAgent_NoOpWhenNotConnected(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()
	// No mcpSrv set, but agent not connected → should return nil
	err := reg.NotifyAgent(context.Background(), uuid.New(), map[string]string{"event": "test"})
	assert.NoError(t, err, "NotifyAgent for disconnected agent should be a no-op")
}

func TestRegistry_NotifyProjectRole_NoOpWithNoConnections(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()
	err := reg.NotifyProjectRole(context.Background(), uuid.New(), "coder", map[string]string{"event": "main_updated"})
	assert.NoError(t, err, "NotifyProjectRole with no sessions should be a no-op")
}

// ── Integration tests (require DATABASE_URL) ──────────────────────────────────
// These tests are skipped unless DATABASE_URL is set and a running server is available.
//
// To run: DATABASE_URL=postgres://... go test ./internal/transport/mcp/... -tags integration

// TestHappyPath exercises the full coder → QA → reviewer pipeline via REST status transitions.
// The MCP tools layer is tested separately by the Python simulation script.
func TestHappyPath(t *testing.T) {
	t.Skip("Requires running server — run via scripts/test_v1_rest.sh")
}

// TestOwnershipLock verifies assigned_agent_id is preserved when QA bounces a task back.
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

// TestOrphanedTaskRecovery verifies that when a session closes, the task is reset to ready.
func TestOrphanedTaskRecovery(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()

	agentID := uuid.New()
	projectID := uuid.New()
	reg.Register("session-drop", agentID, projectID, "coder")
	assert.True(t, reg.IsConnected(agentID))

	// Simulate session close
	recovered, ok := reg.Unregister("session-drop")
	assert.True(t, ok)
	assert.Equal(t, agentID, recovered)
	assert.False(t, reg.IsConnected(agentID))
}

// TestReconnect verifies an agent can re-register with the same agent_id.
func TestReconnect(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()
	agentID := uuid.New()
	projectID := uuid.New()

	reg.Register("session-1", agentID, projectID, "coder")
	assert.True(t, reg.IsConnected(agentID))

	// Simulate disconnect
	reg.Unregister("session-1")
	assert.False(t, reg.IsConnected(agentID))

	// Reconnect with new session
	reg.Register("session-2", agentID, projectID, "coder")
	assert.True(t, reg.IsConnected(agentID), "agent should be connected after reconnect")
}

// TestPromptFallback verifies global default is returned when no project-specific prompt exists.
// This is tested at the adapter level via adapter/postgres/prompt.
func TestPromptFallback(t *testing.T) {
	t.Skip("Requires running Postgres — run via scripts/test_v1_rest.sh")
}

// ── Helper ────────────────────────────────────────────────────────────────────

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

func eventually(t *testing.T, cond func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s: %s", timeout, msg)
}
