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
	pgtask "github.com/alanyang/agent-mesh/internal/adapter/postgres/task"
	pgthread "github.com/alanyang/agent-mesh/internal/adapter/postgres/thread"

	ghclient "github.com/alanyang/agent-mesh/internal/adapter/github"

	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	distsvc "github.com/alanyang/agent-mesh/internal/service/distributor"
	gitsvc "github.com/alanyang/agent-mesh/internal/service/git"
	projectsvc "github.com/alanyang/agent-mesh/internal/service/project"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"

	"github.com/alanyang/agent-mesh/internal/transport"
)

// App holds the top-level resources needed to run and gracefully stop the server.
type App struct {
	Pool     *pgxpool.Pool
	Server   *http.Server
	AgentSvc *agentsvc.Service
}

// Build is the composition root: it reads configuration, connects to the
// database, constructs every adapter and service, and returns a ready-to-start App.
func Build(ctx context.Context) (*App, error) {
	// ── Database ────────────────────────────────────────────────────────
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://agentmesh:agentmesh@localhost:5432/agentmesh?sslmode=disable"
	}
	pool, err := pgdb.Connect(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	// ── Adapters ────────────────────────────────────────────────────────
	taskRepo := pgtask.New(pool)
	threadRepo := pgthread.New(pool)
	agentRepo := pgagent.New(pool)
	eventBus := pgeventbus.New(pool)

	// GitHub adapter – only initialised when all required env vars are set.
	ghToken := os.Getenv("GITHUB_TOKEN")
	ghOwner := os.Getenv("GITHUB_OWNER")
	ghRepoName := os.Getenv("GITHUB_REPO")
	var gitProvider *ghclient.Client
	if ghToken != "" && ghOwner != "" && ghRepoName != "" {
		gitProvider = ghclient.NewClient(ghToken, ghOwner, ghRepoName)
	}

	// ── Services ────────────────────────────────────────────────────────
	distSvc := distsvc.NewService(taskRepo, agentRepo)
	taskSvc := tasksvc.NewService(taskRepo, eventBus, distSvc, threadRepo)
	threadSvc := threadsvc.NewService(threadRepo, eventBus)
	agentSvc := agentsvc.NewService(agentRepo, taskRepo, eventBus)

	var gitSvcInstance *gitsvc.Service
	if gitProvider != nil {
		gitSvcInstance = gitsvc.NewService(gitProvider)
	}

	projectRepo := pgproject.New(pool)
	projectSvcInstance := projectsvc.NewService(projectRepo)

	// ── Transport ───────────────────────────────────────────────────────
	router := transport.NewRouter(taskSvc, threadSvc, agentSvc, gitSvcInstance, projectSvcInstance, eventBus)

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
		Pool:     pool,
		Server:   server,
		AgentSvc: agentSvc,
	}, nil
}
