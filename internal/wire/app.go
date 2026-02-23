package wire

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	pgdb "github.com/alanyang/agent-mesh/internal/adapter/postgres"
	pgagent "github.com/alanyang/agent-mesh/internal/adapter/postgres/agent"
	pgeventbus "github.com/alanyang/agent-mesh/internal/adapter/postgres/eventbus"
	pgproject "github.com/alanyang/agent-mesh/internal/adapter/postgres/project"
	pgprompt "github.com/alanyang/agent-mesh/internal/adapter/postgres/prompt"
	pgtask "github.com/alanyang/agent-mesh/internal/adapter/postgres/task"
	pgthread "github.com/alanyang/agent-mesh/internal/adapter/postgres/thread"

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"

	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	distsvc "github.com/alanyang/agent-mesh/internal/service/distributor"
	projectsvc "github.com/alanyang/agent-mesh/internal/service/project"
	promptsvc "github.com/alanyang/agent-mesh/internal/service/prompt"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"

	"github.com/alanyang/agent-mesh/internal/transport"
	mcptransport "github.com/alanyang/agent-mesh/internal/transport/mcp"
)

// App holds the top-level resources needed to run and gracefully stop the server.
type App struct {
	Pool      *pgxpool.Pool
	Server    *http.Server
	AgentSvc  *agentsvc.Service
	MCPServer *mcptransport.Server
}

// Build is the composition root: the ONLY place concrete types are wired to their
// interface dependencies. All service dependencies flow in as ports.
// [DIP] High-level modules (services) receive abstractions; Build provides concretions.
func Build(ctx context.Context) (*App, error) {
	// ── Database ─────────────────────────────────────────────────────────────
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL not set")
	}
	pool, err := pgdb.Connect(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// ── Adapters ─────────────────────────────────────────────────────────────
	// taskRepo implements both port/task.Repository and port/agent.AgentAvailabilityReader.
	taskRepo := pgtask.New(pool)
	threadRepo := pgthread.New(pool)
	agentRepo := pgagent.New(pool)
	promptRepo := pgprompt.New(pool)
	eventBus := pgeventbus.New(pool)
	projectRepo := pgproject.New(pool)

	// ── Services ─────────────────────────────────────────────────────────────

	// 1. Distributor depends only on AgentAvailabilityReader (ISP).
	distSvc := distsvc.NewService(taskRepo)

	// 2. Thread + agent + prompt services have no inter-service dependencies.
	threadSvc := threadsvc.NewService(threadRepo, eventBus)
	agentSvc := agentsvc.NewService(agentRepo, taskRepo, eventBus)
	promptSvcInstance := promptsvc.NewService(promptRepo)
	projectSvcInstance := projectsvc.NewService(projectRepo)

	// 3. Create the SessionRegistry before taskSvc — taskSvc needs it as notifier ports.
	//    The registry's MCPServer reference is set by mcptransport.New() below.
	reg := mcptransport.NewSessionRegistry()

	// 4. TaskSvc receives pipeline.Config and notifier ports.
	//    [DIP] taskSvc never imports the MCP transport package — it depends on ports.
	taskSvc := tasksvc.NewService(
		taskRepo,
		eventBus,
		distSvc,
		threadRepo,
		reg, // implements port/notifier.AgentNotifier
		reg, // implements port/notifier.RoleNotifier
		pipeline.DefaultConfig,
	)

	// 5. MCP transport server — injects the mcp-go server into reg and registers tools/prompts.
	mcpServer := mcptransport.New(reg, taskSvc, agentSvc, threadSvc, promptSvcInstance)

	// ── Transport ─────────────────────────────────────────────────────────────
	router := transport.NewRouter(
		ctx,
		taskSvc,
		threadSvc,
		agentSvc,
		projectSvcInstance,
		promptSvcInstance,
		mcpServer,
		eventBus,
	)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	server := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	slog.Info("application wired", "port", port)

	return &App{
		Pool:      pool,
		Server:    server,
		AgentSvc:  agentSvc,
		MCPServer: mcpServer,
	}, nil
}
