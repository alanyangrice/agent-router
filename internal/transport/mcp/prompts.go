package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	mcpmcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	promptsvc "github.com/alanyang/agent-mesh/internal/service/prompt"
)

// RegisterPrompts registers the 3 MCP native prompts (coder, qa, reviewer).
// [SRP] Prompt registration only — separated from server lifecycle and tool definitions.
// [OCP] Add a new role prompt by adding a new AddPrompt call — no other files change.
func RegisterPrompts(s *mcpserver.MCPServer, promptSvc *promptsvc.Service) {
	for _, role := range []string{"coder", "qa", "reviewer"} {
		r := role // capture loop variable
		s.AddPrompt(
			mcpmcp.NewPrompt(r,
				mcpmcp.WithPromptDescription(fmt.Sprintf("System prompt for %s agents. Fetched once at session startup.", r)),
				mcpmcp.WithArgument("project_id",
					mcpmcp.ArgumentDescription("Project UUID for project-specific prompt override. Falls back to global default."),
					mcpmcp.RequiredArgument(),
				),
			),
			promptHandler(r, promptSvc),
		)
	}
}

func promptHandler(role string, promptSvc *promptsvc.Service) mcpserver.PromptHandlerFunc {
	return func(ctx context.Context, req mcpmcp.GetPromptRequest) (*mcpmcp.GetPromptResult, error) {
		projectIDStr := req.Params.Arguments["project_id"]

		projectID, err := uuid.Parse(projectIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid project_id: %w", err)
		}

		prompt, err := promptSvc.GetForRole(ctx, projectID, role)
		if err != nil {
			return nil, fmt.Errorf("get prompt for role %s: %w", role, err)
		}

		return mcpmcp.NewGetPromptResult(
			fmt.Sprintf("System prompt for %s agents", role),
			[]mcpmcp.PromptMessage{
				mcpmcp.NewPromptMessage(
					mcpmcp.RoleUser,
					mcpmcp.TextContent{
						Type: "text",
						Text: prompt.Content,
					},
				),
			},
		), nil
	}
}
