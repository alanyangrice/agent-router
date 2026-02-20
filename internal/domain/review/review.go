package review

import (
	"time"

	"github.com/google/uuid"
)

type Verdict string

const (
	VerdictApprove        Verdict = "approve"
	VerdictRequestChanges Verdict = "request_changes"
	VerdictComment        Verdict = "comment"
)

type CommentType string

const (
	CommentSuggestion CommentType = "suggestion"
	CommentIssue      CommentType = "issue"
	CommentNitpick    CommentType = "nitpick"
	CommentQuestion   CommentType = "question"
)

type Comment struct {
	File string      `json:"file"`
	Line int         `json:"line"`
	Body string      `json:"body"`
	Type CommentType `json:"type"`
}

type Review struct {
	ID              uuid.UUID `json:"id"`
	TaskID          uuid.UUID `json:"task_id"`
	ReviewerAgentID uuid.UUID `json:"reviewer_agent_id"`
	PRUrl           string    `json:"pr_url"`
	Verdict         Verdict   `json:"verdict"`
	Comments        []Comment `json:"comments"`
	CreatedAt       time.Time `json:"created_at"`
}

func New(taskID, reviewerAgentID uuid.UUID, prURL string, verdict Verdict, comments []Comment) Review {
	return Review{
		ID:              uuid.New(),
		TaskID:          taskID,
		ReviewerAgentID: reviewerAgentID,
		PRUrl:           prURL,
		Verdict:         verdict,
		Comments:        comments,
		CreatedAt:       time.Now().UTC(),
	}
}
