package task_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	. "github.com/alanyang/agent-mesh/internal/domain/task"
)

func TestCanTransitionTo(t *testing.T) {
	tests := []struct {
		name string
		from Status
		to   Status
		want bool
	}{
		// Valid forward edges
		{name: "backlog→ready",          from: StatusBacklog,    to: StatusReady,       want: true},
		{name: "ready→in_progress",      from: StatusReady,      to: StatusInProgress,  want: true},
		{name: "ready→backlog",          from: StatusReady,      to: StatusBacklog,     want: true},
		{name: "in_progress→in_qa",      from: StatusInProgress, to: StatusInQA,        want: true},
		{name: "in_progress→ready",      from: StatusInProgress, to: StatusReady,       want: true},
		{name: "in_qa→in_review",        from: StatusInQA,       to: StatusInReview,    want: true},
		{name: "in_qa→in_progress",      from: StatusInQA,       to: StatusInProgress,  want: true},
		{name: "in_review→merged",       from: StatusInReview,   to: StatusMerged,      want: true},
		{name: "in_review→in_progress",  from: StatusInReview,   to: StatusInProgress,  want: true},

		// Merged is terminal
		{name: "merged→in_progress invalid", from: StatusMerged, to: StatusInProgress, want: false},
		{name: "merged→in_review invalid",   from: StatusMerged, to: StatusInReview,   want: false},
		{name: "merged→in_qa invalid",       from: StatusMerged, to: StatusInQA,       want: false},
		{name: "merged→ready invalid",       from: StatusMerged, to: StatusReady,      want: false},
		{name: "merged→backlog invalid",     from: StatusMerged, to: StatusBacklog,    want: false},

		// Backlog cannot skip stages
		{name: "backlog→in_progress invalid", from: StatusBacklog, to: StatusInProgress, want: false},
		{name: "backlog→in_qa invalid",       from: StatusBacklog, to: StatusInQA,       want: false},
		{name: "backlog→in_review invalid",   from: StatusBacklog, to: StatusInReview,   want: false},
		{name: "backlog→merged invalid",      from: StatusBacklog, to: StatusMerged,     want: false},

		// in_qa cannot go back past in_progress
		{name: "in_qa→ready invalid",   from: StatusInQA, to: StatusReady,   want: false},
		{name: "in_qa→backlog invalid",  from: StatusInQA, to: StatusBacklog, want: false},
		{name: "in_qa→merged invalid",   from: StatusInQA, to: StatusMerged,  want: false},

		// in_review cannot skip back
		{name: "in_review→ready invalid",   from: StatusInReview, to: StatusReady,   want: false},
		{name: "in_review→backlog invalid",  from: StatusInReview, to: StatusBacklog, want: false},
		{name: "in_review→in_qa invalid",    from: StatusInReview, to: StatusInQA,    want: false},

		// in_progress cannot skip stages
		{name: "in_progress→in_review invalid", from: StatusInProgress, to: StatusInReview, want: false},
		{name: "in_progress→merged invalid",    from: StatusInProgress, to: StatusMerged,   want: false},
		{name: "in_progress→backlog invalid",   from: StatusInProgress, to: StatusBacklog,  want: false},

		// Self-transitions are never valid
		{name: "backlog self-transition",     from: StatusBacklog,    to: StatusBacklog,    want: false},
		{name: "ready self-transition",       from: StatusReady,      to: StatusReady,      want: false},
		{name: "in_progress self-transition", from: StatusInProgress, to: StatusInProgress, want: false},
		{name: "in_qa self-transition",       from: StatusInQA,       to: StatusInQA,       want: false},
		{name: "in_review self-transition",   from: StatusInReview,   to: StatusInReview,   want: false},
		{name: "merged self-transition",      from: StatusMerged,     to: StatusMerged,     want: false},

		// Unknown status — nil allowed list, always false
		{name: "unknown→ready is false",    from: Status("garbage"), to: StatusReady,       want: false},
		{name: "unknown→unknown is false",  from: Status("garbage"), to: Status("garbage"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.from.CanTransitionTo(tt.to))
		})
	}
}
