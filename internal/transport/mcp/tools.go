package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	mcpmcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
)

// RegisterTools registers all 7 MCP tools on the server.
// [SRP] Tool registration only.
// [OCP] Add a new tool by adding a new AddTool call — server.go never changes.
func RegisterTools(
	s *mcpserver.MCPServer,
	reg *SessionRegistry,
	taskSvc *tasksvc.Service,
	agentSvc *agentsvc.Service,
	threadSvc *threadsvc.Service,
) {
	s.AddTool(mcpmcp.NewTool("register_agent",
		mcpmcp.WithDescription("Register this agent with the platform. Call once at startup. Returns the agent_id."),
		mcpmcp.WithString("project_id", mcpmcp.Required(), mcpmcp.Description("Project UUID")),
		mcpmcp.WithString("role", mcpmcp.Required(), mcpmcp.Description("Agent role: coder, qa, or reviewer")),
		mcpmcp.WithString("name", mcpmcp.Required(), mcpmcp.Description("Human-readable agent name")),
		mcpmcp.WithString("model", mcpmcp.Required(), mcpmcp.Description("LLM model identifier")),
	), registerAgentHandler(s, reg, agentSvc))

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
	), postMessageHandler(taskSvc, threadSvc))

	s.AddTool(mcpmcp.NewTool("list_messages",
		mcpmcp.WithDescription("Read the full thread history for a task. Use to re-read QA or reviewer feedback."),
		mcpmcp.WithString("task_id", mcpmcp.Required(), mcpmcp.Description("Task UUID")),
	), listMessagesHandler(taskSvc, threadSvc))
}

// ── Tool handlers ─────────────────────────────────────────────────────────

func registerAgentHandler(srv *mcpserver.MCPServer, reg *SessionRegistry, agentSvc *agentsvc.Service) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		projectIDStr := mcpmcp.ParseString(req, "project_id", "")
		role := mcpmcp.ParseString(req, "role", "")
		name := mcpmcp.ParseString(req, "name", "")
		model := mcpmcp.ParseString(req, "model", "")

		projectID, err := uuid.Parse(projectIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid project_id"), nil
		}

		agent, err := agentSvc.Register(ctx, projectID, role, name, model, []string{})
		if err != nil {
			return mcpmcp.NewToolResultText(fmt.Sprintf("error: %s", err)), nil
		}

		// Map this MCP session to the new agent.
		session := mcpserver.ClientSessionFromContext(ctx)
		if session != nil {
			reg.Register(session.SessionID(), agent.ID, projectID, role)
		}

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

		if agent.CurrentTaskID == nil {
			// Check for ready-assigned tasks
			status := domaintask.StatusReady
			tasks, err := taskSvc.List(ctx, domaintask.ListFilters{
				ProjectID:  &agent.ProjectID,
				AssignedTo: &agentID,
				Status:     &status,
			})
			if err != nil || len(tasks) == 0 {
				return mcpmcp.NewToolResultText("null"), nil
			}
			data, _ := json.Marshal(tasks[0])
			return mcpmcp.NewToolResultText(string(data)), nil
		}

		t, err := taskSvc.GetByID(ctx, *agent.CurrentTaskID)
		if err != nil {
			return mcpmcp.NewToolResultText("null"), nil
		}

		data, _ := json.Marshal(t)
		return mcpmcp.NewToolResultText(string(data)), nil
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

		// Find the task thread and get its messages.
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

func postMessageHandler(taskSvc *tasksvc.Service, threadSvc *threadsvc.Service) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpmcp.CallToolRequest) (*mcpmcp.CallToolResult, error) {
		taskIDStr := mcpmcp.ParseString(req, "task_id", "")
		content := mcpmcp.ParseString(req, "content", "")
		postTypeStr := mcpmcp.ParseString(req, "post_type", "progress")
		agentIDStr := mcpmcp.ParseString(req, "agent_id", "")

		taskID, err := uuid.Parse(taskIDStr)
		if err != nil {
			return mcpmcp.NewToolResultText("error: invalid task_id"), nil
		}

		// Find the task thread.
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

func listMessagesHandler(taskSvc *tasksvc.Service, threadSvc *threadsvc.Service) mcpserver.ToolHandlerFunc {
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
