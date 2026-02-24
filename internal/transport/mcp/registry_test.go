package mcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	mcptransport "github.com/alanyang/agent-mesh/internal/transport/mcp"
)

func TestNotifyAgent_AgentOffline_NoOp(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()

	// Calling NotifyAgent for an agent with no active session returns nil — no panic.
	err := reg.NotifyAgent(context.Background(), uuid.New(), map[string]string{"event": "test"})
	assert.NoError(t, err, "NotifyAgent for disconnected agent must be a no-op")
}

func TestNotifyProjectRole_NoSessions_NoOp(t *testing.T) {
	reg := mcptransport.NewSessionRegistry()

	err := reg.NotifyProjectRole(context.Background(), uuid.New(), "coder", map[string]string{"event": "main_updated"})
	assert.NoError(t, err, "NotifyProjectRole with no sessions must be a no-op")
}
