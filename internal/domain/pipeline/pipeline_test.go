package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
)

func TestEffectiveFreedRole_ExplicitFreedRole(t *testing.T) {
	action := pipeline.StageAction{AssignRole: "qa", FreedRole: "custom"}
	assert.Equal(t, "custom", action.EffectiveFreedRole())
}

func TestEffectiveFreedRole_FallbackToAssignRole(t *testing.T) {
	action := pipeline.StageAction{AssignRole: "qa"} // FreedRole is empty
	assert.Equal(t, "qa", action.EffectiveFreedRole())
}

func TestEffectiveFreedRole_BothEmpty(t *testing.T) {
	action := pipeline.StageAction{}
	assert.Equal(t, "", action.EffectiveFreedRole())
}

func TestDefaultConfig_AllEntries(t *testing.T) {
	cfg := pipeline.DefaultConfig

	// Snapshot all five entries — any field change breaks this test immediately.
	ready := cfg[domaintask.StatusReady]
	assert.Equal(t, "coder", ready.AssignRole)
	assert.Equal(t, "", ready.FreedRole)
	assert.Equal(t, "", ready.BroadcastEvent)
	assert.Equal(t, "", ready.BroadcastRole)

	inProgress := cfg[domaintask.StatusInProgress]
	assert.Equal(t, "", inProgress.AssignRole)
	assert.Equal(t, "coder", inProgress.FreedRole)
	assert.Equal(t, "", inProgress.BroadcastEvent)
	assert.Equal(t, "", inProgress.BroadcastRole)

	inQA := cfg[domaintask.StatusInQA]
	assert.Equal(t, "qa", inQA.AssignRole)
	assert.Equal(t, "qa", inQA.FreedRole)
	assert.Equal(t, "", inQA.BroadcastEvent)
	assert.Equal(t, "", inQA.BroadcastRole)

	inReview := cfg[domaintask.StatusInReview]
	assert.Equal(t, "reviewer", inReview.AssignRole)
	assert.Equal(t, "reviewer", inReview.FreedRole)
	assert.Equal(t, "", inReview.BroadcastEvent)
	assert.Equal(t, "", inReview.BroadcastRole)

	merged := cfg[domaintask.StatusMerged]
	assert.Equal(t, "", merged.AssignRole)
	assert.Equal(t, "", merged.FreedRole)
	assert.Equal(t, "main_updated", merged.BroadcastEvent)
	assert.Equal(t, "coder", merged.BroadcastRole)
}

func TestDefaultConfig_InProgressFreedRole(t *testing.T) {
	action := pipeline.DefaultConfig[domaintask.StatusInProgress]
	// Explicit FreedRole takes priority — EffectiveFreedRole fallback NOT triggered here.
	assert.Equal(t, "coder", action.FreedRole, "FreedRole must be 'coder'")
	assert.Equal(t, "", action.AssignRole, "AssignRole must be empty")
	// EffectiveFreedRole still returns "coder" (explicit wins over fallback).
	assert.Equal(t, "coder", action.EffectiveFreedRole())
}

func TestDefaultConfig_InQADerived(t *testing.T) {
	action := pipeline.DefaultConfig[domaintask.StatusInQA]
	assert.Equal(t, "qa", action.FreedRole, "FreedRole is set explicitly")
	assert.Equal(t, "qa", action.AssignRole)
	// EffectiveFreedRole returns the explicit FreedRole value.
	assert.Equal(t, "qa", action.EffectiveFreedRole())
}

func TestDefaultConfig_HasExactlyFiveEntries(t *testing.T) {
	assert.Len(t, pipeline.DefaultConfig, 5, "DefaultConfig must have exactly 5 entries")
}
