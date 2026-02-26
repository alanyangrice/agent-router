package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
)

func TestEffectiveFreedRole(t *testing.T) {
	tests := []struct {
		name     string
		action   pipeline.StageAction
		wantRole string
	}{
		{
			name:     "explicit FreedRole takes priority over AssignRole",
			action:   pipeline.StageAction{AssignRole: "qa", FreedRole: "custom"},
			wantRole: "custom",
		},
		{
			name:     "fallback to AssignRole when FreedRole is empty",
			action:   pipeline.StageAction{AssignRole: "qa"},
			wantRole: "qa",
		},
		{
			name:     "both empty returns empty string",
			action:   pipeline.StageAction{},
			wantRole: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantRole, tt.action.EffectiveFreedRole())
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Run("HasExactlyFiveEntries", func(t *testing.T) {
		assert.Len(t, pipeline.DefaultConfig, 5)
	})

	tests := []struct {
		name                   string
		status                 domaintask.Status
		wantAssignRole         string
		wantFreedRole          string
		wantBroadcastEvent     string
		wantBroadcastRole      string
		wantEffectiveFreedRole string
	}{
		{
			name:                   "ready: coder assigned, no freed/broadcast",
			status:                 domaintask.StatusReady,
			wantAssignRole:         "coder",
			wantFreedRole:          "",
			wantBroadcastEvent:     "",
			wantBroadcastRole:      "",
			wantEffectiveFreedRole: "coder",
		},
		{
			name:                   "in_progress: coder freed explicitly, no assign",
			status:                 domaintask.StatusInProgress,
			wantAssignRole:         "",
			wantFreedRole:          "coder",
			wantBroadcastEvent:     "",
			wantBroadcastRole:      "",
			wantEffectiveFreedRole: "coder",
		},
		{
			name:                   "in_qa: qa assigned and freed",
			status:                 domaintask.StatusInQA,
			wantAssignRole:         "qa",
			wantFreedRole:          "qa",
			wantBroadcastEvent:     "",
			wantBroadcastRole:      "",
			wantEffectiveFreedRole: "qa",
		},
		{
			name:                   "in_review: reviewer assigned and freed",
			status:                 domaintask.StatusInReview,
			wantAssignRole:         "reviewer",
			wantFreedRole:          "reviewer",
			wantBroadcastEvent:     "",
			wantBroadcastRole:      "",
			wantEffectiveFreedRole: "reviewer",
		},
		{
			name:                   "merged: broadcast to coder, no assign or free",
			status:                 domaintask.StatusMerged,
			wantAssignRole:         "",
			wantFreedRole:          "",
			wantBroadcastEvent:     "main_updated",
			wantBroadcastRole:      "coder",
			wantEffectiveFreedRole: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, ok := pipeline.DefaultConfig[tt.status]
			require.True(t, ok, "status %s must have a pipeline action", tt.status)
			assert.Equal(t, tt.wantAssignRole, action.AssignRole, "AssignRole")
			assert.Equal(t, tt.wantFreedRole, action.FreedRole, "FreedRole")
			assert.Equal(t, tt.wantBroadcastEvent, action.BroadcastEvent, "BroadcastEvent")
			assert.Equal(t, tt.wantBroadcastRole, action.BroadcastRole, "BroadcastRole")
			assert.Equal(t, tt.wantEffectiveFreedRole, action.EffectiveFreedRole(), "EffectiveFreedRole()")
		})
	}
}
