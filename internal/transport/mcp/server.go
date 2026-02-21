package mcp

import (
	"context"
	"log/slog"
	"net/http"

	mcpserver "github.com/mark3labs/mcp-go/server"

	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	promptsvc "github.com/alanyang/agent-mesh/internal/service/prompt"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
)

// Server wraps the mark3labs/mcp-go MCPServer and its StreamableHTTPServer.
// [SRP] HTTP/SSE server lifecycle only (start, stop, session open/close).
//
//	Tools are registered in tools.go, prompts in prompts.go, session state in registry.go.
//
// [OCP] Adding new tools or prompts never requires changes to this file.
type Server struct {
	httpSrv  *mcpserver.StreamableHTTPServer
	reg      *SessionRegistry
	agentSvc *agentsvc.Service
}

// New creates the MCP transport server.
// The reg parameter is a pre-built SessionRegistry (created before taskSvc in the wire).
// The MCPServer reference is set on the registry after construction.
func New(
	reg *SessionRegistry,
	taskSvc *tasksvc.Service,
	agentSvc *agentsvc.Service,
	threadSvc *threadsvc.Service,
	promptSvc *promptsvc.Service,
) *Server {
	s := &Server{
		reg:      reg,
		agentSvc: agentSvc,
	}

	hooks := &mcpserver.Hooks{}
	hooks.OnUnregisterSession = append(hooks.OnUnregisterSession, s.onSessionClose)

	mcpSrv := mcpserver.NewMCPServer(
		"agent-mesh",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithPromptCapabilities(true),
		mcpserver.WithHooks(hooks),
	)

	// Inject the mcp-go server into the registry (breaks the init cycle).
	reg.SetMCPServer(mcpSrv)

	RegisterTools(mcpSrv, reg, taskSvc, agentSvc, threadSvc)
	RegisterPrompts(mcpSrv, promptSvc)

	s.httpSrv = mcpserver.NewStreamableHTTPServer(mcpSrv)
	return s
}

// Handler returns an http.Handler that serves the MCP SSE endpoint.
func (s *Server) Handler() http.Handler {
	return s.httpSrv
}

// Registry returns the session registry (implements AgentNotifier + RoleNotifier).
func (s *Server) Registry() *SessionRegistry {
	return s.reg
}

func (s *Server) onSessionClose(ctx context.Context, session mcpserver.ClientSession) {
	agentID, ok := s.reg.Unregister(session.SessionID())
	if !ok {
		return
	}
	slog.InfoContext(ctx, "mcp: session closed, reaping agent", "session_id", session.SessionID(), "agent_id", agentID)
	go s.agentSvc.ReapOrphaned(context.WithoutCancel(ctx), agentID)
}
