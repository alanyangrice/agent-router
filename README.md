# Agent Router

An AI agent collaboration platform — orchestration, coordination, and observability for autonomous AI agents working on software projects.

## What It Does

- **Task Board** — create, assign, and track tasks with dependencies (DAG)
- **Agent Lifecycle** — register agents, monitor heartbeats, auto-recover stalled work
- **Communication Threads** — per-task and system threads with scoped visibility
- **Git Coordination** — open PRs, merge, post review comments via GitHub
- **Real-Time Dashboard** — Kanban board, thread viewer, agent status, activity timeline

## Architecture

Hexagonal architecture (ports and adapters) in Go. Agents are Python processes that communicate with the Go server via REST API. LLM interactions are delegated to an external proxy (LiteLLM, OpenRouter, etc.).

## Quick Start

### Prerequisites

- Go 1.22+
- Docker & Docker Compose
- Node.js 20+ (for dashboard)
- Python 3.11+ (for agents)

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
| `GITHUB_TOKEN` | GitHub Personal Access Token | Yes |
| `DATABASE_URL` | Postgres connection string (overrides config) | No |

## Project Structure

```
cmd/server/          Entry point
internal/
  domain/            Pure domain models
  port/              Interfaces (contracts)
  service/           Business logic
  adapter/           Interface implementations (Postgres, GitHub, etc.)
  transport/         HTTP handlers + WebSocket
  wire/              Dependency injection
agent-sdk/           Python SDK for agents
agents/              Agent implementations
web/                 Next.js dashboard
configs/             YAML configuration
migrations/          SQL migration files
```

## Tech Stack

- **Backend:** Go, Gin, sqlc, Postgres
- **Frontend:** Next.js, React, TypeScript
- **Agents:** Python
- **Real-time:** WebSocket + Postgres LISTEN/NOTIFY
