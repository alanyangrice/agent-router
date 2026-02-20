package event

import (
	"time"

	"github.com/google/uuid"
)

type Type string

const (
	TypeTaskCreated    Type = "task_created"
	TypeTaskUpdated    Type = "task_updated"
	TypeTaskAssigned   Type = "task_assigned"
	TypeTaskCompleted  Type = "task_completed"
	TypeThreadMessage  Type = "thread_message"
	TypeAgentOnline    Type = "agent_online"
	TypeAgentOffline   Type = "agent_offline"
	TypeAgentHeartbeat Type = "agent_heartbeat"
	TypePROpened       Type = "pr_opened"
	TypePRMerged       Type = "pr_merged"
	TypeReviewPosted   Type = "review_posted"
)

// Event carries identifiers only, not full state.
// Subscribers fetch fresh state from the appropriate repository.
type Event struct {
	Type      Type      `json:"type"`
	EntityID  uuid.UUID `json:"entity_id"`
	Timestamp time.Time `json:"timestamp"`
}

func New(eventType Type, entityID uuid.UUID) Event {
	return Event{
		Type:      eventType,
		EntityID:  entityID,
		Timestamp: time.Now().UTC(),
	}
}
