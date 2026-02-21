package notifier

import (
	"context"

	"github.com/google/uuid"
)

// RoleNotifier broadcasts an event to all connected agents of a given role in a project.
// [ISP] Separated from AgentNotifier — task service only needs role-based broadcast.
// [DIP] Task service depends on this abstraction, not on the MCP transport.
// [LSP] The in-memory SessionRegistry (V1) and Redis pub/sub (V3) both satisfy this interface.
type RoleNotifier interface {
	NotifyProjectRole(ctx context.Context, projectID uuid.UUID, role string, event any) error
}
