package transport

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	porteventbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	gitsvc "github.com/alanyang/agent-mesh/internal/service/git"
	projectsvc "github.com/alanyang/agent-mesh/internal/service/project"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"

	agenthandler "github.com/alanyang/agent-mesh/internal/transport/agent"
	githandler "github.com/alanyang/agent-mesh/internal/transport/git"
	projecthandler "github.com/alanyang/agent-mesh/internal/transport/project"
	taskhandler "github.com/alanyang/agent-mesh/internal/transport/task"
	threadhandler "github.com/alanyang/agent-mesh/internal/transport/thread"
	wshandler "github.com/alanyang/agent-mesh/internal/transport/ws"
)

func NewRouter(
	ctx context.Context,
	taskSvc *tasksvc.Service,
	threadSvc *threadsvc.Service,
	agentSvc *agentsvc.Service,
	gitSvc *gitsvc.Service,
	projectSvc *projectsvc.Service,
	eventBus porteventbus.EventBus,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(RequestLogger())
	r.Use(CORSMiddleware())
	r.Use(IdempotencyMiddleware())

	api := r.Group("/api")

	projecthandler.Register(api.Group("/projects"), projectSvc)
	taskhandler.Register(api.Group("/tasks"), taskSvc)
	threadhandler.Register(api.Group("/threads"), threadSvc)
	agenthandler.Register(api.Group("/agents"), agentSvc)
	githandler.Register(api.Group("/git"), gitSvc)

	hub := wshandler.NewHub()
	hub.Register(api.Group("/ws"))

	// Bridge: one subscription per domain channel (4 total Postgres connections).
	// All events within a channel are forwarded to WS clients; event.Type in the
	// payload lets the client filter. AgentHeartbeat is excluded — it is in the
	// agent channel but carries no actionable state for browsers or agents.
	for _, ch := range []event.Channel{
		event.ChannelTask,
		event.ChannelAgent,
		event.ChannelThread,
		event.ChannelGit,
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
