package notifier

import (
	"context"

	"github.com/google/uuid"
)

// AgentNotifier pushes an event to a specific agent's active session.
// [ISP] Separated from RoleNotifier — consumers declare only what they use.
type AgentNotifier interface {
	NotifyAgent(ctx context.Context, agentID uuid.UUID, event any) error
}
