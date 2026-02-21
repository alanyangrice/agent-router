# Agent Mesh — Curl Commands Reference

Copy the **Setup** block first to set variables, then run any command below.

---

## Setup — set these once per terminal session

```bash
export BASE="http://localhost:8080"

# Fill these in after creating a project and task:
export PROJECT_ID="<your-project-id>"
export TASK_ID="<your-task-id>"
export AGENT_ID="<your-agent-id>"
```

---

## Projects

### Create project
```bash
curl -s -X POST "$BASE/api/projects/" \
  -H "Content-Type: application/json" \
  -d '{"name": "My Project", "repo_url": "https://github.com/placeholder/repo"}' | jq .
```

### Get project
```bash
curl -s "$BASE/api/projects/$PROJECT_ID" | jq .
```

---

## Tasks

### Create task
```bash
curl -s -X POST "$BASE/api/tasks/" \
  -H "Content-Type: application/json" \
  -d "{
    \"project_id\": \"$PROJECT_ID\",
    \"title\": \"Test task\",
    \"description\": \"A test task for manual testing\",
    \"priority\": \"medium\",
    \"branch_type\": \"feature\",
    \"created_by\": \"human\"
  }" | jq .
```

### List all tasks
```bash
curl -s "$BASE/api/tasks/" | jq .
```

### List tasks by status
```bash
# Options: backlog | ready | in_progress | in_review | done
curl -s "http://localhost:8080/api/tasks/?status=in_progress" | jq .
```

### List tasks for a project
```bash
curl -s "http://localhost:8080/api/tasks/?project_id=$PROJECT_ID" | jq .
```

### Get task detail
```bash
curl -s "http://localhost:8080/api/tasks/$TASK_ID" | jq .
```

### Move task: backlog → ready (triggers auto-distribution)
```bash
curl -s -X PATCH "http://localhost:8080/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from": "backlog", "status_to": "ready"}' | jq .
```

### Move task: ready → in_progress (manual override)
```bash
curl -s -X PATCH "http://localhost:8080/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from": "ready", "status_to": "in_progress"}' | jq .
```

### Move task: in_progress → in_review
```bash
curl -s -X PATCH "http://localhost:8080/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from": "in_progress", "status_to": "in_review"}' | jq .
```

### Move task: in_review → done
```bash
curl -s -X PATCH "http://localhost:8080/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from": "in_review", "status_to": "done"}' | jq .
```

### Reset task: in_progress → ready (e.g. to re-trigger distribution)
```bash
curl -s -X PATCH "http://localhost:8080/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from": "in_progress", "status_to": "ready"}' | jq .
```

---

## Agents

### Register agent
```bash
curl -s -X POST "http://localhost:8080/api/agents/register" \
  -H "Content-Type: application/json" \
  -d "{
    \"project_id\": \"$PROJECT_ID\",
    \"role\": \"coder\",
    \"name\": \"Test Agent\",
    \"model\": \"gpt-4o\",
    \"skills\": [\"go\", \"python\"]
  }" | jq .
```

### List all agents
```bash
curl -s "$BASE/api/agents/" | jq .
```

### List idle agents
```bash
curl -s "$BASE/api/agents/?status=idle" | jq .
```

### Get agent detail
```bash
curl -s "$BASE/api/agents/$AGENT_ID" | jq .
```

### Send heartbeat
```bash
curl -s -X POST "$BASE/api/agents/$AGENT_ID/heartbeat" | jq .
```

---

## Threads

### List threads for a project
```bash
curl -s "$BASE/api/threads/?project_id=$PROJECT_ID" | jq .
```

### List threads for a task
```bash
curl -s "$BASE/api/threads/?task_id=$TASK_ID" | jq .
```

### Create thread manually
```bash
curl -s -X POST "$BASE/api/threads/" \
  -H "Content-Type: application/json" \
  -d "{
    \"project_id\": \"$PROJECT_ID\",
    \"task_id\": \"$TASK_ID\",
    \"type\": \"task\",
    \"name\": \"Task thread\"
  }" | jq .
```

### Get messages in a thread
```bash
export THREAD_ID="<your-thread-id>"
curl -s "$BASE/api/threads/$THREAD_ID/messages" | jq .
```

### Post a message to a thread
```bash
curl -s -X POST "$BASE/api/threads/$THREAD_ID/messages" \
  -H "Content-Type: application/json" \
  -d '{"post_type": "comment", "content": "Hello from human"}' | jq .
```

---

## Common workflows

### Full happy path: create task and watch it get assigned
```bash
# 1. Create task (saves ID to TASK_ID)
TASK_ID=$(curl -s -X POST "$BASE/api/tasks/" \
  -H "Content-Type: application/json" \
  -d "{\"project_id\":\"$PROJECT_ID\",\"title\":\"Auto test\",\"description\":\"Testing auto-assign\",\"priority\":\"medium\",\"branch_type\":\"feature\",\"created_by\":\"human\"}" \
  | jq -r '.id')
echo "Task ID: $TASK_ID"

# 2. Move to ready — distributor auto-assigns and transitions to in_progress
curl -s -X PATCH "$BASE/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from": "backlog", "status_to": "ready"}' | jq '{id:.id, status:.status, assigned_agent_id:.assigned_agent_id}'
```

### Check everything at once
```bash
echo "=== Tasks ===" && curl -s "$BASE/api/tasks/?project_id=$PROJECT_ID" | jq '[.[] | {id:.id, title:.title, status:.status, assigned:.assigned_agent_id}]'
echo "=== Agents ===" && curl -s "$BASE/api/agents/?project_id=$PROJECT_ID" | jq '[.[] | {id:.id, name:.name, status:.status, current_task:.current_task_id}]'
```
