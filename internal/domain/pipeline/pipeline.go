package pipeline

import (
	"github.com/alanyang/agent-mesh/internal/domain/task"
)

// StageAction defines what the service should do when a task enters a given status.
// This drives the task service without hardcoding pipeline logic in the service itself (OCP).
type StageAction struct {
	// AssignRole is the agent role to assign the task to. Empty means no new assignment
	// (e.g. bouncing back to in_progress preserves the existing coder assignment).
	AssignRole string

	// BroadcastEvent is the event name to broadcast to all agents of a specific role
	// via RoleNotifier. Empty means no broadcast.
	BroadcastEvent string
}

// Config maps each task status to the action the service should take on entry.
type Config map[task.Status]StageAction

// DefaultConfig is the V1 three-role pipeline.
// To add a new stage (e.g. V2 architect): extend this map — no service code changes required.
var DefaultConfig = Config{
	task.StatusReady: {
		AssignRole: "coder",
	},
	task.StatusInProgress: {
		// No new assignment: preserve existing coder (ownership lock).
		// The task service is responsible for NOT clearing assigned_agent_id on this transition.
	},
	task.StatusInQA: {
		AssignRole: "qa",
	},
	task.StatusInReview: {
		AssignRole: "reviewer",
	},
	task.StatusMerged: {
		// Terminal: no new assignment. Broadcast main_updated to all in-progress coders.
		BroadcastEvent: "main_updated",
	},
}
