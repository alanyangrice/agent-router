#!/usr/bin/env bash
set -euo pipefail

# Agent Mesh E2E Validation Script
# Prerequisites: Postgres running (docker compose up -d), server running (go run cmd/server/main.go)

BASE_URL="${AGENT_MESH_URL:-http://localhost:8080}"

echo "=== Agent Mesh E2E Validation ==="
echo "Server: $BASE_URL"
echo ""

# 1. Create a project
echo "1. Creating project..."
PROJECT=$(curl -sf -X POST "$BASE_URL/api/projects" \
  -H "Content-Type: application/json" \
  -d '{"name": "test-project", "repo_url": "https://github.com/test/repo"}')
PROJECT_ID=$(echo "$PROJECT" | python3 -c "import sys, json; print(json.load(sys.stdin)['id'])")
echo "   Project created: $PROJECT_ID"

# 2. Create a task
echo "2. Creating task..."
TASK=$(curl -sf -X POST "$BASE_URL/api/tasks" \
  -H "Content-Type: application/json" \
  -d "{\"project_id\": \"$PROJECT_ID\", \"title\": \"Add health check endpoint\", \"description\": \"Add a /healthz endpoint to the server\", \"priority\": \"high\", \"branch_type\": \"feature\", \"created_by\": \"human\"}")
TASK_ID=$(echo "$TASK" | python3 -c "import sys, json; print(json.load(sys.stdin)['id'])")
TASK_STATUS=$(echo "$TASK" | python3 -c "import sys, json; print(json.load(sys.stdin)['status'])")
echo "   Task created: $TASK_ID (status: $TASK_STATUS)"

# 3. Register an agent
echo "3. Registering agent..."
AGENT=$(curl -sf -X POST "$BASE_URL/api/agents/register" \
  -H "Content-Type: application/json" \
  -d "{\"project_id\": \"$PROJECT_ID\", \"role\": \"coder\", \"name\": \"Test Coder\", \"model\": \"test\", \"skills\": [\"go\", \"python\"]}")
AGENT_ID=$(echo "$AGENT" | python3 -c "import sys, json; print(json.load(sys.stdin)['id'])")
echo "   Agent registered: $AGENT_ID"

# 4. Send heartbeat
echo "4. Sending heartbeat..."
curl -sf -X POST "$BASE_URL/api/agents/$AGENT_ID/heartbeat" > /dev/null
echo "   Heartbeat sent"

# 5. Transition task: backlog -> ready
echo "5. Moving task to ready..."
curl -sf -X PATCH "$BASE_URL/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from": "backlog", "status_to": "ready"}' > /dev/null
echo "   Task moved to ready"

# 6. Transition task: ready -> in_progress
echo "6. Moving task to in_progress..."
curl -sf -X PATCH "$BASE_URL/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from": "ready", "status_to": "in_progress"}' > /dev/null
echo "   Task moved to in_progress"

# 7. List threads for the task (created automatically or check)
echo "7. Listing threads..."
THREADS=$(curl -sf "$BASE_URL/api/threads?project_id=$PROJECT_ID")
echo "   Threads: $THREADS"

# 8. Get task detail
echo "8. Verifying task state..."
TASK_DETAIL=$(curl -sf "$BASE_URL/api/tasks/$TASK_ID")
FINAL_STATUS=$(echo "$TASK_DETAIL" | python3 -c "import sys, json; print(json.load(sys.stdin)['status'])")
echo "   Task status: $FINAL_STATUS"

# 9. List agents
echo "9. Listing agents..."
AGENTS=$(curl -sf "$BASE_URL/api/agents?project_id=$PROJECT_ID")
AGENT_COUNT=$(echo "$AGENTS" | python3 -c "import sys, json; print(len(json.load(sys.stdin)))")
echo "   Active agents: $AGENT_COUNT"

# 10. Verify agent detail
echo "10. Verifying agent detail..."
AGENT_DETAIL=$(curl -sf "$BASE_URL/api/agents/$AGENT_ID")
AGENT_STATUS=$(echo "$AGENT_DETAIL" | python3 -c "import sys, json; print(json.load(sys.stdin)['status'])")
echo "    Agent status: $AGENT_STATUS"

echo ""
echo "=== E2E Validation Complete ==="
echo "All API endpoints responded successfully."
echo ""
echo "To run the full flow with an agent:"
echo "  1. docker compose up -d"
echo "  2. psql and run migrations/001_initial.sql"  
echo "  3. go run cmd/server/main.go"
echo "  4. AGENT_MESH_PROJECT_ID=$PROJECT_ID python agents/example/agent.py"
echo "  5. Open http://localhost:3000 for the dashboard"
