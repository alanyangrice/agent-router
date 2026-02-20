package transport

import (
	"github.com/gin-gonic/gin"

	"github.com/alanyang/agent-mesh/internal/port/eventbus"
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
	taskSvc *tasksvc.Service,
	threadSvc *threadsvc.Service,
	agentSvc *agentsvc.Service,
	gitSvc *gitsvc.Service,
	projectSvc *projectsvc.Service,
	_ eventbus.EventBus,
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

	return r
}
