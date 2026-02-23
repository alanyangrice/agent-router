# Agent Router

An MCP-first agent coordination platform — orchestration, coordination, and observability for autonomous AI agents working on software projects. Built on hexagonal architecture and SOLID principles.

## What It Does

- **Three-role pipeline** — coder → QA → reviewer/merger, driven by configuration not code
- **MCP-native agents** — agents connect via Model Context Protocol (SSE), no custom SDK required
- **Task state machine** — CAS transitions, dependency DAG, ownership lock so the same coder fixes their own feedback
- **Role prompts** — per-role, per-project system prompts editable from the dashboard without redeployment
- **Real-time dashboard** — 6-column Kanban (backlog / ready / in_progress / in_qa / in_review / merged), thread viewer, agent status panel
- **Push-based, zero polling** — agents sleep on the SSE stream and are woken by `task_assigned` notifications

## The Pipeline

```
backlog
  └─ ready              assigned to: coder
       └─ in_progress   coder: writes code, pushes branch, opens PR, records pr_url
            └─ in_qa             assigned to: qa
                 ├─ (fail) ──────→ in_progress   same coder re-assigned (ownership lock)
                 └─ (pass) ──────→ in_review      assigned to: reviewer
                                        ├─ (changes needed) → in_progress   same coder re-assigned
                                        └─ (approved) ──────→ merged        broadcast: main_updated
```

Pipeline routing is configuration, not code. `service/task.go` reads `pipeline.Config[to]` to decide what to do after each transition — no switch statements, no hardcoded stages. Adding a new stage extends the config map only.

## Architecture

Hexagonal (ports and adapters) in Go. High-level modules depend on abstractions; concrete types are wired only at the composition root (`wire/app.go`).

```
agent-mesh/
├── cmd/server/
├── internal/
│   ├── domain/
│   │   ├── task/          pure model + state machine
│   │   ├── thread/        pure model
│   │   ├── agent/         pure model
│   │   ├── prompt/        RolePrompt model
│   │   └── pipeline/      Config map (stage → action)
│   ├── port/
│   │   ├── task/
│   │   ├── thread/
│   │   ├── agent/
│   │   │   └── availability.go   GetAvailable(projectID, role)
│   │   ├── notifier/
│   │   │   ├── agent.go          NotifyAgent (per-agent push)
│   │   │   └── role.go           NotifyProjectRole (role broadcast)
│   │   ├── prompt/               PromptRepository
│   │   ├── distributor/
│   │   └── eventbus/
│   ├── adapter/postgres/
│   │   ├── task/
│   │   ├── thread/
│   │   ├── agent/
│   │   ├── prompt/
│   │   └── eventbus/
│   ├── service/
│   │   ├── task/          state management + pipeline routing
│   │   ├── thread/
│   │   ├── agent/         lifecycle + orphan recovery
│   │   ├── prompt/        project→global fallback resolution
│   │   └── distributor/   agent selection by role
│   ├── transport/
│   │   ├── task/  thread/  agent/  project/  prompt/  ws/
│   │   └── mcp/
│   │       ├── server.go      HTTP/SSE lifecycle only
│   │       ├── registry.go    session storage + notification dispatch
│   │       ├── tools.go       7 MCP tool registrations
│   │       └── prompts.go     3 MCP prompt registrations
│   └── wire/              composition root (only place concrete types meet interfaces)
├── web/                   Next.js dashboard
├── migrations/            SQL migration files
├── scripts/
│   └── test_v1_rest.sh    REST API smoke test (25 checks)
└── agents/example/
    └── simulate_pipeline.py   MCP pipeline simulation (3 scenarios)
```

## Quick Start

### Prerequisites

- Go 1.22+
- Docker & Docker Compose
- Node.js 20+ (for dashboard)
- Python 3.11+ + `pip install mcp httpx` (for simulation script)

### Run

```bash
# Start Postgres
docker compose up -d

# Run migrations
go run cmd/server/main.go migrate

# Start the server
go run cmd/server/main.go

# In another terminal, start the dashboard
cd web && npm install && npm run dev
```

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `DATABASE_URL` | Postgres connection string (overrides config) | No |

## Connecting an Agent

Agents connect via MCP over SSE. No custom SDK — any MCP client works.

**Claude Desktop (`~/.config/claude/claude_desktop_config.json`):**

```json
{
  "mcpServers": {
    "agent-mesh": {
      "url": "http://localhost:8080/mcp",
      "env": { "AGENT_ROLE": "coder", "PROJECT_ID": "<uuid>" }
    }
  }
}
```

**Agent startup sequence:**

1. Open SSE connection to `/mcp`
2. `register_agent(project_id, role, name, model)` → store the returned `agent_id` permanently
3. `prompts/get(name="coder", arguments={"project_id": "..."})` → use as LLM system prompt
4. `claim_task(agent_id)` → task or null; sleep on SSE stream if null
5. When `task_assigned` push arrives, call `claim_task` again → receive the task
6. `get_task_context(task_id)` → full brief + thread history
7. Do the work, use MCP tools to report back and advance the pipeline

**Reconnect after crash:** pass the stored `agent_id` to `register_agent` — the existing agent record is reactivated and `claim_task` returns the still-assigned task immediately.

## MCP Tools

| Tool | Description |
|------|-------------|
| `register_agent(project_id, role, name, model [, agent_id])` | Register or reactivate an agent; returns `agent_id` |
| `claim_task(agent_id)` | Claim the next available task for this agent's role |
| `get_task_context(task_id)` | Full task brief + thread history |
| `update_task_status(task_id, from, to)` | Advance the pipeline (CAS); triggers routing |
| `set_pr_url(task_id, pr_url)` | Store PR URL on the task |
| `post_message(task_id, content, post_type)` | Write to task thread; fires WebSocket event |
| `list_messages(task_id)` | Read thread history |

**Post types:** `progress`, `review_feedback`, `blocker`, `artifact`, `comment`

## MCP Prompts

`prompts/get` is a native MCP call, not a tool. Three prompts are registered: `coder`, `qa`, `reviewer`. Content is served from the `role_prompts` table — project-specific first, falling back to the global default.

Edit prompts without redeployment: `PUT /api/projects/:id/prompts/:role`

## REST API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/projects/` | Create project |
| `GET` | `/api/projects/:id` | Get project |
| `POST` | `/api/tasks/` | Create task |
| `GET` | `/api/tasks/` | List tasks (`?project_id=`) |
| `GET` | `/api/tasks/:id` | Get task |
| `PATCH` | `/api/tasks/:id` | Update task status |
| `POST` | `/api/tasks/:id/dependencies` | Add dependency |
| `GET` | `/api/agents/` | List agents (`?project_id=`) |
| `GET` | `/api/agents/:id` | Get agent |
| `GET` | `/api/threads/` | List threads (`?task_id=`) |
| `POST` | `/api/threads/:id/messages` | Post message |
| `GET` | `/api/threads/:id/messages` | List messages |
| `GET` | `/api/projects/:id/prompts/:role` | Get role prompt |
| `PUT` | `/api/projects/:id/prompts/:role` | Set role prompt |
| `GET` | `/api/ws` | WebSocket event stream |

> Agents register via `register_agent` MCP tool only — `POST /api/agents/register` does not exist.

## Testing

```bash
# REST API smoke test (25 checks)
bash scripts/test_v1_rest.sh

# MCP pipeline simulation (happy path, ownership lock, main_updated broadcast)
PROJECT_ID=<uuid> python agents/example/simulate_pipeline.py

# Go integration tests
go test ./internal/transport/mcp/...
```

## Tech Stack

- **Backend:** Go, Gin, pgx, Postgres
- **Frontend:** Next.js, React, TypeScript
- **Transport:** MCP/SSE (`mark3labs/mcp-go`) + REST + WebSocket
- **Real-time:** Postgres LISTEN/NOTIFY (EventBus) + in-memory SSE session registry

## Roadmap

| Version | Goal |
|---------|------|
| **V1 (current)** | Human creates tasks; agents execute three-role pipeline via MCP |
| **V2** | Architect agent decomposes uploaded spec into tasks; human approves before pipeline starts |
| **V3** | Conversational intake, agent memory (pgvector), Redis EventBus, cost tracking, trust levels |
