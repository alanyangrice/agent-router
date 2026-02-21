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

// broadcastedEvents are the event types forwarded to WebSocket clients.
// AgentHeartbeat is intentionally excluded — it fires every 30s per agent and
// carries no actionable information for browser or agent subscribers.
var broadcastedEvents = []event.Type{
	event.TypeTaskCreated,
	event.TypeTaskUpdated,
	event.TypeTaskAssigned,
	event.TypeTaskCompleted,
	event.TypeThreadMessage,
	event.TypeAgentOnline,
	event.TypeAgentOffline,
	event.TypePROpened,
	event.TypePRMerged,
	event.TypeReviewPosted,
}

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

	// Bridge: subscribe to backend events and broadcast to all WS clients.
	for _, topic := range broadcastedEvents {
		t := topic
		if _, err := eventBus.Subscribe(ctx, t, func(_ context.Context, e event.Event) {
			hub.Broadcast(e)
		}); err != nil {
			slog.Error("failed to subscribe event to WS hub", "topic", t, "error", err)
		}
	}

	return r
}
