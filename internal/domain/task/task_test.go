package task_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	. "github.com/alanyang/agent-mesh/internal/domain/task"
)

func TestCanTransitionTo_ValidEdges(t *testing.T) {
	cases := []struct{ from, to Status }{
		{StatusBacklog, StatusReady},
		{StatusReady, StatusInProgress},
		{StatusReady, StatusBacklog},
		{StatusInProgress, StatusInQA},
		{StatusInProgress, StatusReady},
		{StatusInQA, StatusInReview},
		{StatusInQA, StatusInProgress},
		{StatusInReview, StatusMerged},
		{StatusInReview, StatusInProgress},
	}
	for _, tc := range cases {
		assert.True(t, tc.from.CanTransitionTo(tc.to), "%s → %s should be valid", tc.from, tc.to)
	}
}

func TestCanTransitionTo_InvalidEdges(t *testing.T) {
	cases := []struct{ from, to Status }{
		// merged is terminal
		{StatusMerged, StatusInProgress},
		{StatusMerged, StatusInReview},
		{StatusMerged, StatusInQA},
		{StatusMerged, StatusReady},
		{StatusMerged, StatusBacklog},
		// backlog cannot skip stages
		{StatusBacklog, StatusInProgress},
		{StatusBacklog, StatusInQA},
		{StatusBacklog, StatusInReview},
		{StatusBacklog, StatusMerged},
		// in_qa cannot go back past in_progress
		{StatusInQA, StatusReady},
		{StatusInQA, StatusBacklog},
		{StatusInQA, StatusMerged},
		// in_review cannot skip back
		{StatusInReview, StatusReady},
		{StatusInReview, StatusBacklog},
		{StatusInReview, StatusInQA},
		// in_progress cannot skip
		{StatusInProgress, StatusInReview},
		{StatusInProgress, StatusMerged},
		{StatusInProgress, StatusBacklog},
	}
	for _, tc := range cases {
		assert.False(t, tc.from.CanTransitionTo(tc.to), "%s → %s should be invalid", tc.from, tc.to)
	}
}

func TestCanTransitionTo_SelfTransitions(t *testing.T) {
	statuses := []Status{
		StatusBacklog, StatusReady, StatusInProgress,
		StatusInQA, StatusInReview, StatusMerged,
	}
	for _, s := range statuses {
		assert.False(t, s.CanTransitionTo(s), "%s → %s self-transition should be false", s, s)
	}
}

func TestCanTransitionTo_UnknownStatus(t *testing.T) {
	// Unknown status has nil allowed list — range over nil is safe, returns false.
	unknown := Status("garbage")
	assert.False(t, unknown.CanTransitionTo(StatusReady), "unknown → ready should be false")
	assert.False(t, unknown.CanTransitionTo(unknown), "unknown → unknown should be false")
}
