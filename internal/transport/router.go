package transport

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	porteventbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	promptsvc "github.com/alanyang/agent-mesh/internal/service/prompt"
	projectsvc "github.com/alanyang/agent-mesh/internal/service/project"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
	mcptransport "github.com/alanyang/agent-mesh/internal/transport/mcp"

	agenthandler "github.com/alanyang/agent-mesh/internal/transport/agent"
	projecthandler "github.com/alanyang/agent-mesh/internal/transport/project"
	prompthandler "github.com/alanyang/agent-mesh/internal/transport/prompt"
	taskhandler "github.com/alanyang/agent-mesh/internal/transport/task"
	threadhandler "github.com/alanyang/agent-mesh/internal/transport/thread"
	wshandler "github.com/alanyang/agent-mesh/internal/transport/ws"
)

func NewRouter(
	ctx context.Context,
	taskSvc *tasksvc.Service,
	threadSvc *threadsvc.Service,
	agentSvc *agentsvc.Service,
	projectSvc *projectsvc.Service,
	promptSvc *promptsvc.Service,
	mcpServer *mcptransport.Server,
	eventBus porteventbus.EventBus,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(RequestLogger())
	r.Use(CORSMiddleware())

	api := r.Group("/api")

	projecthandler.Register(api.Group("/projects"), projectSvc)
	taskhandler.Register(api.Group("/tasks"), taskSvc)
	threadhandler.Register(api.Group("/threads"), threadSvc)
	agenthandler.Register(api.Group("/agents"), agentSvc)

	// Prompt editor endpoints: GET/PUT /api/projects/:id/prompts/:role
	api.Group("/projects/:id/prompts").Any("/*role", func(c *gin.Context) {
		// strip the leading slash from the role wildcard
		role := c.Param("role")
		if len(role) > 1 {
			c.Params = append(c.Params, gin.Param{Key: "role", Value: role[1:]})
		}
		c.Next()
	})
	prompthandler.Register(api.Group("/projects/:id/prompts"), promptSvc)

	hub := wshandler.NewHub()
	hub.Register(api.Group("/ws"))

	// MCP server at /mcp
	r.Any("/mcp", gin.WrapH(mcpServer.Handler()))
	r.Any("/mcp/*path", gin.WrapH(mcpServer.Handler()))

	// Bridge: forward Postgres NOTIFY events to the WebSocket hub for dashboard.
	for _, ch := range []event.Channel{
		event.ChannelTask,
		event.ChannelAgent,
		event.ChannelThread,
	} {
		c := ch
		if _, err := eventBus.Subscribe(ctx, c, func(_ context.Context, e event.Event) {
			if e.Type == event.TypeAgentHeartbeat {
				return
			}
			hub.Broadcast(e)
		}); err != nil {
			slog.Error("failed to subscribe channel to WS hub", "channel", c, "error", err)
		}
	}

	return r
}
