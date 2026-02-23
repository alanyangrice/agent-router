package pipeline

import (
	"github.com/alanyang/agent-mesh/internal/domain/task"
)

// StageAction defines what the service should do when a task enters or leaves a given status.
type StageAction struct {
	// AssignRole is the agent role to assign the task to when entering this status.
	// Empty means no new assignment.
	AssignRole string

	// FreedRole is the agent role released when a task leaves this status.
	// It triggers SweepUnassigned so waiting tasks get picked up immediately.
	FreedRole string

	// BroadcastEvent is the event name to broadcast on entry to this status.
	BroadcastEvent string

	// BroadcastRole is the role that receives the broadcast. Replaces the hardcoded "coder".
	BroadcastRole string
}

// Config maps each task status to the action the service should take on entry.
type Config map[task.Status]StageAction

// DefaultConfig is the V1 three-role pipeline (coder → QA → reviewer → merged).
// V1.1: FreedRole drives sweep triggers so agents get work immediately after freeing a slot.
// To add a new stage: extend this map — no service code changes required (OCP).
var DefaultConfig = Config{
	task.StatusReady: {
		AssignRole: "coder",
	},
	task.StatusInProgress: {
		// No new assignment on entry — coder was already assigned at the ready stage.
		// FreedRole: "coder" because leaving in_progress frees a coder slot.
		FreedRole: "coder",
	},
	task.StatusInQA: {
		AssignRole: "qa",
		FreedRole:  "qa", // leaving in_qa frees a qa slot
	},
	task.StatusInReview: {
		AssignRole: "reviewer",
		FreedRole:  "reviewer",
	},
	task.StatusMerged: {
		BroadcastEvent: "main_updated",
		BroadcastRole:  "coder",
	},
}
