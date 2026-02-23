package wire

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	porteventbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
)

// startReaper subscribes to the agent event channel and schedules a grace-period
// timer whenever an agent goes offline. If the agent reconnects within the grace
// period the timer is cancelled. If it expires, in-flight tasks are released and
// the freed roles are swept for waiting work.
//
// It also runs a startup orphan scan so timers lost during a process restart are
// rescheduled immediately (with a shorter startup grace).
func startReaper(ctx context.Context, app *App, bus porteventbus.EventBus) {
	reaperGrace := envDuration("REAPER_GRACE_SECONDS", 5*time.Minute)
	startupGrace := envDuration("STARTUP_REAPER_GRACE_SECONDS", 30*time.Second)

	var (
		mu     sync.Mutex
		timers = make(map[uuid.UUID]*time.Timer)
	)

	// scheduleReapWithGrace is the canonical timer body — defined once.
	// scheduleReap is a thin wrapper that passes the live-disconnect grace period.
	scheduleReapWithGrace := func(agentID uuid.UUID, grace time.Duration) {
		t := time.AfterFunc(grace, func() {
			mu.Lock()
			delete(timers, agentID)
			mu.Unlock()

			// ReleaseAgent returns projectID alongside freed statuses in a single
			// GetByID call — no redundant round-trip inside the sweep loop.
			projectID, freedStatuses, err := app.AgentSvc.ReleaseAgent(context.Background(), agentID)
			if err != nil {
				slog.Error("reaper: release agent failed", "agent_id", agentID, "error", err)
				return
			}
			for _, status := range freedStatuses {
				if action, ok := pipeline.DefaultConfig[status]; ok {
					if role := action.EffectiveFreedRole(); role != "" {
						go func() {
							if err := app.TaskSvc.SweepUnassigned(context.Background(), projectID, role); err != nil {
								slog.Error("reaper: sweep failed after release", "role", role, "error", err)
							}
						}()
					}
				}
			}
		})
		mu.Lock()
		timers[agentID] = t
		mu.Unlock()
	}

	scheduleReap := func(agentID uuid.UUID) {
		scheduleReapWithGrace(agentID, reaperGrace)
	}

	// Subscribe to the agent channel: schedule on offline, cancel on online.
	if _, err := bus.Subscribe(ctx, event.ChannelAgent, func(_ context.Context, e event.Event) {
		switch e.Type {
		case event.TypeAgentOffline:
			scheduleReap(e.EntityID)
		case event.TypeAgentOnline:
			mu.Lock()
			if t, ok := timers[e.EntityID]; ok {
				t.Stop()
				delete(timers, e.EntityID)
			}
			mu.Unlock()
		}
	}); err != nil {
		slog.Error("reaper: failed to subscribe to agent channel", "error", err)
	}

	// Startup orphan scan: agents that were offline when the process restarted
	// already missed their live-disconnect timer. Use a shorter grace so stale
	// tasks are not held for the full 5 minutes after a deployment restart.
	offlineAgents, err := app.AgentSvc.ListOfflineWithInflightTasks(ctx)
	if err != nil {
		slog.Error("reaper: startup scan failed", "error", err)
	}
	for _, agentID := range offlineAgents {
		scheduleReapWithGrace(agentID, startupGrace)
	}
	if len(offlineAgents) > 0 {
		slog.Info("reaper: startup scan scheduled timers for orphaned agents", "count", len(offlineAgents))
	}
}

// envDuration reads an integer-seconds env var and returns a Duration.
// Falls back to defaultVal if the var is unset or invalid.
func envDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return defaultVal
}
