//go:build integration

package testutil

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// NotifyCall records a single notification delivered by captureNotifier.
type NotifyCall struct {
	AgentID   uuid.UUID
	ProjectID uuid.UUID
	Role      string
	Event     any
}

// CaptureNotifier is a test-double that implements both AgentNotifier and RoleNotifier.
// It records every call with a mutex so it is safe for concurrent use.
type CaptureNotifier struct {
	mu    sync.Mutex
	Calls []NotifyCall
}

func (c *CaptureNotifier) NotifyAgent(_ context.Context, agentID uuid.UUID, event any) error {
	c.mu.Lock()
	c.Calls = append(c.Calls, NotifyCall{AgentID: agentID, Event: event})
	c.mu.Unlock()
	return nil
}

func (c *CaptureNotifier) NotifyProjectRole(_ context.Context, projectID uuid.UUID, role string, event any) error {
	c.mu.Lock()
	c.Calls = append(c.Calls, NotifyCall{ProjectID: projectID, Role: role, Event: event})
	c.mu.Unlock()
	return nil
}

// AgentNotifications returns all calls made for a specific agentID.
func (c *CaptureNotifier) AgentNotifications(agentID uuid.UUID) []NotifyCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []NotifyCall
	for _, call := range c.Calls {
		if call.AgentID == agentID {
			out = append(out, call)
		}
	}
	return out
}

// RoleNotifications returns all broadcast calls for a specific role.
func (c *CaptureNotifier) RoleNotifications(role string) []NotifyCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []NotifyCall
	for _, call := range c.Calls {
		if call.Role == role {
			out = append(out, call)
		}
	}
	return out
}

// Reset clears all recorded calls.
func (c *CaptureNotifier) Reset() {
	c.mu.Lock()
	c.Calls = nil
	c.mu.Unlock()
}
