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

// Channel is a domain-scoped Postgres NOTIFY channel.
// All event types within a domain share one LISTEN connection.
type Channel string

const (
	ChannelTask   Channel = "task"
	ChannelAgent  Channel = "agent"
	ChannelThread Channel = "thread"
	ChannelGit    Channel = "git"
)

var typeToChannel = map[Type]Channel{
	TypeTaskCreated:    ChannelTask,
	TypeTaskUpdated:    ChannelTask,
	TypeTaskAssigned:   ChannelTask,
	TypeTaskCompleted:  ChannelTask,
	TypeAgentOnline:    ChannelAgent,
	TypeAgentOffline:   ChannelAgent,
	TypeAgentHeartbeat: ChannelAgent,
	TypeThreadMessage:  ChannelThread,
	TypePROpened:       ChannelGit,
	TypePRMerged:       ChannelGit,
	TypeReviewPosted:   ChannelGit,
}

// ChannelFor returns the domain channel for a given event type.
func ChannelFor(t Type) Channel { return typeToChannel[t] }

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
