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
	pglocker "github.com/alanyang/agent-mesh/internal/adapter/postgres/locker"
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
	TaskSvc   *tasksvc.Service
	MCPServer *mcptransport.Server
}

// Build is the composition root: the only place concrete types are wired to their
// interface dependencies.
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
	taskRepo := pgtask.New(pool)
	threadRepo := pgthread.New(pool)
	agentRepo := pgagent.New(pool)
	promptRepo := pgprompt.New(pool)
	eventBus := pgeventbus.New(pool)
	projectRepo := pgproject.New(pool)
	locker := pglocker.New(pool)

	// ── Services ─────────────────────────────────────────────────────────────

	// Distributor uses agentRepo.ClaimAgent (atomic SELECT + UPDATE).
	distSvc := distsvc.NewService(agentRepo)

	threadSvcInstance := threadsvc.NewService(threadRepo, eventBus)
	agentSvcInstance := agentsvc.NewService(agentRepo, taskRepo, eventBus)
	promptSvcInstance := promptsvc.NewService(promptRepo)
	projectSvcInstance := projectsvc.NewService(projectRepo)

	reg := mcptransport.NewSessionRegistry()

	taskSvcInstance := tasksvc.NewService(
		taskRepo,
		eventBus,
		distSvc,
		threadRepo,
		reg, // implements port/notifier.AgentNotifier
		reg, // implements port/notifier.RoleNotifier
		pipeline.DefaultConfig,
		locker,
	)

	mcpServer := mcptransport.New(reg, taskSvcInstance, agentSvcInstance, threadSvcInstance, promptSvcInstance)

	// ── Transport ─────────────────────────────────────────────────────────────
	router := transport.NewRouter(
		ctx,
		taskSvcInstance,
		threadSvcInstance,
		agentSvcInstance,
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

	app := &App{
		Pool:      pool,
		Server:    server,
		AgentSvc:  agentSvcInstance,
		TaskSvc:   taskSvcInstance,
		MCPServer: mcpServer,
	}

	// ── Event-Driven Grace-Period Reaper ──────────────────────────────────────
	startReaper(ctx, app, eventBus)

	return app, nil
}
