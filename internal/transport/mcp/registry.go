package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// sessionEntry tracks a connected agent's identity.
type sessionEntry struct {
	agentID   uuid.UUID
	projectID uuid.UUID
	role      string
}

// SessionRegistry is the in-memory registry of active MCP sessions.
// It implements both port/notifier.AgentNotifier and port/notifier.RoleNotifier.
//
// [SRP] Session storage and notification dispatch only.
// [LSP] In V3, a Redis pub/sub implementation satisfies the same port interfaces.
// [DIP] The task service depends on port interfaces, not this concrete type.
type SessionRegistry struct {
	mu         sync.RWMutex
	bySessions map[string]*sessionEntry // sessionID → entry
	byAgent    map[uuid.UUID]string     // agentID → sessionID

	// mcpSrv is set after the MCP server is constructed (avoids circular init dependency).
	mcpMu  sync.RWMutex
	mcpSrv *mcpserver.MCPServer
}

// NewSessionRegistry creates a registry without an MCP server reference.
// Call SetMCPServer once the mcp-go server is constructed.
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		bySessions: make(map[string]*sessionEntry),
		byAgent:    make(map[uuid.UUID]string),
	}
}

// SetMCPServer injects the mcp-go server after construction (breaks the init cycle).
func (r *SessionRegistry) SetMCPServer(s *mcpserver.MCPServer) {
	r.mcpMu.Lock()
	r.mcpSrv = s
	r.mcpMu.Unlock()
}

// Register maps a session to an agent. Called by the register_agent MCP tool.
func (r *SessionRegistry) Register(sessionID string, agentID uuid.UUID, projectID uuid.UUID, role string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if oldSession, ok := r.byAgent[agentID]; ok {
		delete(r.bySessions, oldSession)
	}

	r.bySessions[sessionID] = &sessionEntry{
		agentID:   agentID,
		projectID: projectID,
		role:      role,
	}
	r.byAgent[agentID] = sessionID
}

// Unregister removes a session when it closes. Returns the agentID it mapped to.
func (r *SessionRegistry) Unregister(sessionID string) (uuid.UUID, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.bySessions[sessionID]
	if !ok {
		return uuid.Nil, false
	}

	delete(r.bySessions, sessionID)
	delete(r.byAgent, entry.agentID)
	return entry.agentID, true
}

// NotifyAgent implements port/notifier.AgentNotifier.
func (r *SessionRegistry) NotifyAgent(_ context.Context, agentID uuid.UUID, event any) error {
	r.mu.RLock()
	sessionID, ok := r.byAgent[agentID]
	r.mu.RUnlock()

	if !ok {
		return nil // Agent not connected — no-op.
	}

	r.mcpMu.RLock()
	srv := r.mcpSrv
	r.mcpMu.RUnlock()

	if srv == nil {
		return fmt.Errorf("mcp server not initialized")
	}

	params, err := toParams(event)
	if err != nil {
		return fmt.Errorf("serialize notification: %w", err)
	}

	return srv.SendNotificationToSpecificClient(sessionID, "notifications/message", params)
}

// NotifyProjectRole implements port/notifier.RoleNotifier.
func (r *SessionRegistry) NotifyProjectRole(_ context.Context, projectID uuid.UUID, role string, event any) error {
	params, err := toParams(event)
	if err != nil {
		return fmt.Errorf("serialize notification: %w", err)
	}

	r.mu.RLock()
	targets := make([]string, 0)
	for sessionID, entry := range r.bySessions {
		if entry.role == role && entry.projectID == projectID {
			targets = append(targets, sessionID)
		}
	}
	r.mu.RUnlock()

	r.mcpMu.RLock()
	srv := r.mcpSrv
	r.mcpMu.RUnlock()

	if srv == nil {
		return nil
	}

	var lastErr error
	for _, sessionID := range targets {
		if err := srv.SendNotificationToSpecificClient(sessionID, "notifications/message", params); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// IsConnected returns whether the agent has an active SSE session.
func (r *SessionRegistry) IsConnected(agentID uuid.UUID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byAgent[agentID]
	return ok
}

func toParams(event any) (map[string]any, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	var params map[string]any
	if err := json.Unmarshal(data, &params); err != nil {
		return map[string]any{"data": event}, nil
	}
	return params, nil
}
