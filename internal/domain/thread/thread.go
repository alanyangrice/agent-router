package thread

import (
	"time"

	"github.com/google/uuid"
)

// ThreadType is simplified to task only. Other thread types are V3+ concerns.
type ThreadType string

const (
	TypeTask ThreadType = "task"
)

// PostType covers the five meaningful communication patterns between agents.
type PostType string

const (
	PostProgress       PostType = "progress"
	PostReviewFeedback PostType = "review_feedback"
	PostBlocker        PostType = "blocker"
	PostArtifact       PostType = "artifact"
	PostComment        PostType = "comment"
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
	ID        uuid.UUID  `json:"id"`
	ThreadID  uuid.UUID  `json:"thread_id"`
	AgentID   *uuid.UUID `json:"agent_id,omitempty"`
	PostType  PostType   `json:"post_type"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
}

func NewMessage(threadID uuid.UUID, agentID *uuid.UUID, postType PostType, content string) Message {
	return Message{
		ID:        uuid.New(),
		ThreadID:  threadID,
		AgentID:   agentID,
		PostType:  postType,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
}

type ListFilters struct {
	ProjectID *uuid.UUID
	TaskID    *uuid.UUID
}
