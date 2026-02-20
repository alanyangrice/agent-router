package thread

import (
	"time"

	"github.com/google/uuid"
)

type ThreadType string

const (
	TypeTask         ThreadType = "task"
	TypeGeneral      ThreadType = "general"
	TypeTaskBoard    ThreadType = "task_board"
	TypePRMerging    ThreadType = "pr_merging"
	TypeBlockers     ThreadType = "blockers"
	TypeArchDecision ThreadType = "arch_decision"
	TypeEscalation   ThreadType = "escalation"
)

type PostType string

const (
	PostProgress   PostType = "progress"
	PostBlocker    PostType = "blocker"
	PostHelpWanted PostType = "help_wanted"
	PostDecision   PostType = "decision"
	PostArtifact   PostType = "artifact"
	PostReviewReq  PostType = "review_req"
	PostComment    PostType = "comment"
)

type Thread struct {
	ID        uuid.UUID  `json:"id"`
	ProjectID uuid.UUID  `json:"project_id"`
	TaskID    *uuid.UUID `json:"task_id,omitempty"`
	Type      ThreadType `json:"type"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
}

func New(projectID uuid.UUID, threadType ThreadType, name string, taskID *uuid.UUID) Thread {
	return Thread{
		ID:        uuid.New(),
		ProjectID: projectID,
		TaskID:    taskID,
		Type:      threadType,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
}

type Message struct {
	ID        uuid.UUID              `json:"id"`
	ThreadID  uuid.UUID              `json:"thread_id"`
	AgentID   *uuid.UUID             `json:"agent_id,omitempty"`
	PostType  PostType               `json:"post_type"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

func NewMessage(threadID uuid.UUID, agentID *uuid.UUID, postType PostType, content string) Message {
	return Message{
		ID:        uuid.New(),
		ThreadID:  threadID,
		AgentID:   agentID,
		PostType:  postType,
		Content:   content,
		Metadata:  map[string]interface{}{},
		CreatedAt: time.Now().UTC(),
	}
}

type Visibility struct {
	ThreadID  uuid.UUID `json:"thread_id"`
	AgentRole string    `json:"agent_role"`
}

type ListFilters struct {
	ProjectID *uuid.UUID
	TaskID    *uuid.UUID
	Type      *ThreadType
}
