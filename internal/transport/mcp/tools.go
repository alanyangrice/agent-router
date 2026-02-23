package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	mcpmcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
)

// RegisterTools registers all MCP tools on the server.
// [SRP] Tool registration only.
// [OCP] Add a new tool by adding a new AddTool call — server.go never changes.
func RegisterTools(
	s *mcpserver.MCPServer,
	reg *SessionRegistry,
	taskSvc *tasksvc.Service,
	agentSvc *agentsvc.Service,
	threadSvc *threadsvc.Service,
) {
	// Derive valid roles from pipeline config — not hardcoded.
	validRoles := make(map[string]bool)
	for _, action := range pipeline.DefaultConfig {
		if action.AssignRole != "" {
			validRoles[action.AssignRole] = true
		}
	}

	s.AddTool(mcpmcp.NewTool("register_agent",
		mcpmcp.WithDescription("Register this agent with the platform. Returns the agent_id. On reconnect, pass the previously issued agent_id to reuse the same record instead of creating a new one."),
		mcpmcp.WithString("project_id", mcpmcp.Required(), mcpmcp.Description("Project UUID")),
		mcpmcp.WithString("role", mcpmcp.Required(), mcpmcp.Description("Agent role: coder, qa, or reviewer")),
		mcpmcp.WithString("name", mcpmcp.Required(), mcpmcp.Description("Human-readable agent name")),
		mcpmcp.WithString("model", mcpmcp.Required(), mcpmcp.Description("LLM model identifier")),
		mcpmcp.WithString("agent_id", mcpmcp.Description("Previously issued agent UUID. Pass on reconnect to reuse the existing agent record.")),
	), registerAgentHandler(s, reg, agentSvc, taskSvc, validRoles))

	s.AddTool(mcpmcp.NewTool("claim_task",
		mcpmcp.WithDescription("Returns the task currently assigned to this agent. Returns null if no task is assigned yet — listen on the SSE stream for a task_assigned notification then call again."),
		mcpmcp.WithString("agent_id", mcpmcp.Required(), mcpmcp.Description("Agent UUID returned by register_agent")),
	), claimTaskHandler(taskSvc, agentSvc))

	s.AddTool(mcpmcp.NewTool("get_task_context",
		mcpmcp.WithDescription("Returns full task context: description, branch name, PR URL, dependency statuses, and complete thread history (including QA and reviewer feedback from prior rounds)."),
		mcpmcp.WithString("task_id", mcpmcp.Required(), mcpmcp.Description("Task UUID")),
	), getTaskContextHandler(taskSvc, threadSvc))

	s.AddTool(mcpmcp.NewTool("update_task_status",
		mcpmcp.WithDescription("Advance the task status. The service handles pipeline routing automatically. Valid transitions: in_progress→in_qa, in_qa→in_review or in_qa→in_progress, in_review→merged or in_review→in_progress."),
		mcpmcp.WithString("task_id", mcpmcp.Required(), mcpmcp.Description("Task UUID")),
		mcpmcp.WithString("from", mcpmcp.Required(), mcpmcp.Description("Current status (CAS guard)")),
		mcpmcp.WithString("to", mcpmcp.Required(), mcpmcp.Description("Target status")),
	), updateTaskStatusHandler(taskSvc))

	s.AddTool(mcpmcp.NewTool("set_pr_url",
		mcpmcp.WithDescription("Record the GitHub PR URL on the task after opening a pull request. Visible on the dashboard."),
		mcpmcp.WithString("task_id", mcpmcp.Required(), mcpmcp.Description("Task UUID")),
		mcpmcp.WithString("pr_url", mcpmcp.Required(), mcpmcp.Description("Full GitHub PR URL")),
	), setPRUrlHandler(taskSvc))

	s.AddTool(mcpmcp.NewTool("post_message",
		mcpmcp.WithDescription("Post a message to the task thread. Appears in the dashboard thread viewer in real time."),
		mcpmcp.WithString("task_id", mcpmcp.Required(), mcpmcp.Description("Task UUID")),
		mcpmcp.WithString("content", mcpmcp.Required(), mcpmcp.Description("Message content")),
		mcpmcp.WithString("post_type", mcpmcp.Required(), mcpmcp.Description("One of: progress, review_feedback, blocker, artifact, comment")),
		mcpmcp.WithString("agent_id", mcpmcp.Description("Agent UUID (optional, used to attribute the message)")),
	), postMessageHandler(threadSvc))

	s.AddTool(mcpmcp.NewTool("list_messages",
		mcpmcp.WithDescription("Read the full thread history for a task. Use to re-read QA or reviewer feedback."),
		mcpmcp.WithString("task_id", mcpmcp.Required(), mcpmcp.Description("Task UUID")),
	), listMessagesHandler(threadSvc))
}

// ── Tool handlers ─────────────────────────────────────────────────────────

func registerAgentHandler(
	srv *mcpserver.MCPServer,
	reg *SessionRegistry,
	agentSvc *agentsvc.Service,
	taskSvc *tasksvc.Service,
	validRoles map[string]bool,
) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		projectIDStr := mcpmcp.ParseString(req, "project_id", "")
		role := mcpmcp.ParseString(req, "role", "")
		name := mcpmcp.ParseString(req, "name", "")
		model := mcpmcp.ParseString(req, "model", "")
		existingIDStr := mcpmcp.ParseString(req, "agent_id", "")

		projectID, err := uuid.Parse(projectIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid project_id"), nil
		}

		// Validate role against pipeline config values.
		if !validRoles[role] {
			return mcpmcp.NewToolResultText("error: invalid role — must be one of: coder, qa, reviewer"), nil
		}

		session := mcpserver.ClientSessionFromContext(ctx)

		// Reconnect path: reactivate existing agent record.
		if existingIDStr != "" {
			if existingID, err := uuid.Parse(existingIDStr); err == nil {
				if agent, err := agentSvc.Reactivate(ctx, existingID); err == nil {
					if session != nil {
						reg.Register(session.SessionID(), agent.ID, agent.ProjectID, agent.Role)
					}
					// Sweep for waiting unassigned tasks of this agent's role.
					go func() {
						if err := taskSvc.SweepUnassigned(context.Background(), agent.ProjectID, agent.Role); err != nil {
							// Non-fatal — log only.
							_ = err
						}
					}()
					result, _ := json.Marshal(map[string]string{"agent_id": agent.ID.String()})
					return mcpmcp.NewToolResultText(string(result)), nil
				}
				// Reactivate failed (agent not found in DB) — fall through to create new.
			}
		}

		// First-time registration: create a new agent record.
		agent, err := agentSvc.Register(ctx, projectID, role, name, model, []string{})
		if err != nil {
			return mcpmcp.NewToolResultText(fmt.Sprintf("error: %s", err)), nil
		}

		if session != nil {
			reg.Register(session.SessionID(), agent.ID, projectID, role)
		}

		// Sweep for waiting unassigned tasks of this agent's role.
		go func() {
			if err := taskSvc.SweepUnassigned(context.Background(), agent.ProjectID, agent.Role); err != nil {
				_ = err
			}
		}()

		result, _ := json.Marshal(map[string]string{"agent_id": agent.ID.String()})
		return mcpmcp.NewToolResultText(string(result)), nil
	}
}

func claimTaskHandler(taskSvc *tasksvc.Service, agentSvc *agentsvc.Service) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		agentIDStr := mcpmcp.ParseString(req, "agent_id", "")
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid agent_id"), nil
		}

		agent, err := agentSvc.GetByID(ctx, agentID)
		if err != nil {
			return mcpmcp.NewToolResultText("error: agent not found"), nil
		}

		// Query all tasks assigned to this agent (oldest first so reconnecting agents
		// resume their earliest in-flight task).
		tasks, err := taskSvc.List(ctx, domaintask.ListFilters{
			ProjectID:   &agent.ProjectID,
			AssignedTo:  &agentID,
			OldestFirst: true,
		})
		if err != nil {
			return mcpmcp.NewToolResultText("null"), nil
		}

		// Return the first non-terminal task.
		for _, t := range tasks {
			if t.Status != domaintask.StatusMerged && t.Status != domaintask.StatusBacklog {
				// SetWorking is a safety net — ClaimAgent already marks working for normal
				// assignments, but bounce-back uses repo.Assign directly. This ensures
				// current_task_id is always set correctly regardless of assignment path.
				agentSvc.SetWorking(ctx, agentID, t.ID)
				data, _ := json.Marshal(t)
				return mcpmcp.NewToolResultText(string(data)), nil
			}
		}

		// No task — agent is now idle. SetIdle then sweep immediately for waiting tasks
		// so the agent gets work as soon as something is available for its role.
		agentSvc.SetIdle(ctx, agentID)
		go func() {
			if err := taskSvc.SweepUnassigned(context.Background(), agent.ProjectID, agent.Role); err != nil {
				_ = err
			}
		}()
		return mcpmcp.NewToolResultText("null"), nil
	}
}

func getTaskContextHandler(taskSvc *tasksvc.Service, threadSvc *threadsvc.Service) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		taskIDStr := mcpmcp.ParseString(req, "task_id", "")
		taskID, err := uuid.Parse(taskIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid task_id"), nil
		}

		t, err := taskSvc.GetByID(ctx, taskID)
		if err != nil {
			return mcpmcp.NewToolResultText(fmt.Sprintf("error: %s", err)), nil
		}

		deps, _ := taskSvc.GetDependencies(ctx, taskID)

		threads, _ := threadSvc.ListThreads(ctx, domainthread.ListFilters{TaskID: &taskID})
		var messages []interface{}
		if len(threads) > 0 {
			msgs, _ := threadSvc.ListMessages(ctx, threads[0].ID)
			for _, m := range msgs {
				messages = append(messages, m)
			}
		}

		result := map[string]interface{}{
			"task":         t,
			"dependencies": deps,
			"thread":       messages,
		}
		data, _ := json.Marshal(result)
		return mcpmcp.NewToolResultText(string(data)), nil
	}
}

func updateTaskStatusHandler(taskSvc *tasksvc.Service) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		taskIDStr := mcpmcp.ParseString(req, "task_id", "")
		fromStr := mcpmcp.ParseString(req, "from", "")
		toStr := mcpmcp.ParseString(req, "to", "")

		taskID, err := uuid.Parse(taskIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid task_id"), nil
		}

		from := domaintask.Status(fromStr)
		to := domaintask.Status(toStr)

		if err := taskSvc.UpdateStatus(ctx, taskID, from, to); err != nil {
			return mcpmcp.NewToolResultText(fmt.Sprintf("error: %s", err)), nil
		}

		return mcpmcp.NewToolResultText(`{"ok":true}`), nil
	}
}

func setPRUrlHandler(taskSvc *tasksvc.Service) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		taskIDStr := mcpmcp.ParseString(req, "task_id", "")
		prURL := mcpmcp.ParseString(req, "pr_url", "")

		taskID, err := uuid.Parse(taskIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid task_id"), nil
		}
		if prURL == "" {
			return mcpmcp.NewToolResultText("error: pr_url required"), nil
		}

		if err := taskSvc.SetPRUrl(ctx, taskID, prURL); err != nil {
			return mcpmcp.NewToolResultText(fmt.Sprintf("error: %s", err)), nil
		}

		return mcpmcp.NewToolResultText(`{"ok":true}`), nil
	}
}

var validPostTypes = map[string]bool{
	"progress":        true,
	"review_feedback": true,
	"blocker":         true,
	"artifact":        true,
	"comment":         true,
}

func postMessageHandler(threadSvc *threadsvc.Service) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		taskIDStr := mcpmcp.ParseString(req, "task_id", "")
		content := mcpmcp.ParseString(req, "content", "")
		postTypeStr := mcpmcp.ParseString(req, "post_type", "progress")
		agentIDStr := mcpmcp.ParseString(req, "agent_id", "")

		taskID, err := uuid.Parse(taskIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid task_id"), nil
		}

		if !validPostTypes[postTypeStr] {
			return mcpmcp.NewToolResultText("error: invalid post_type — must be one of: progress, review_feedback, blocker, artifact, comment"), nil
		}
		if strings.TrimSpace(content) == "" {
			return mcpmcp.NewToolResultText("error: content must not be empty"), nil
		}

		threads, err := threadSvc.ListThreads(ctx, domainthread.ListFilters{TaskID: &taskID})
		if err != nil || len(threads) == 0 {
			return mcpmcp.NewToolResultText("error: task thread not found"), nil
		}

		var agentID *uuid.UUID
		if agentIDStr != "" {
			id, err := uuid.Parse(agentIDStr)
			if err == nil {
				agentID = &id
			}
		}

		postType := domainthread.PostType(postTypeStr)
		msg, err := threadSvc.PostMessage(ctx, threads[0].ID, agentID, postType, content)
		if err != nil {
			return mcpmcp.NewToolResultText(fmt.Sprintf("error: %s", err)), nil
		}

		data, _ := json.Marshal(msg)
		return mcpmcp.NewToolResultText(string(data)), nil
	}
}

func listMessagesHandler(threadSvc *threadsvc.Service) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		taskIDStr := mcpmcp.ParseString(req, "task_id", "")
		taskID, err := uuid.Parse(taskIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid task_id"), nil
		}

		threads, err := threadSvc.ListThreads(ctx, domainthread.ListFilters{TaskID: &taskID})
		if err != nil || len(threads) == 0 {
			return mcpmcp.NewToolResultText("[]"), nil
		}

		messages, err := threadSvc.ListMessages(ctx, threads[0].ID)
		if err != nil {
			return mcpmcp.NewToolResultText("[]"), nil
		}

		data, _ := json.Marshal(messages)
		return mcpmcp.NewToolResultText(string(data)), nil
	}
}
