#!/usr/bin/env bash
# V1 REST API smoke test
# Usage: BASE_URL=http://localhost:8080 bash scripts/test_v1_rest.sh
# Requires: curl, jq
set -euo pipefail

BASE="${BASE_URL:-http://localhost:8080}"
PASS=0
FAIL=0

assert() {
  local desc="$1" actual="$2" expected="$3"
  if [ "$actual" = "$expected" ]; then
    echo "  PASS $desc"
    ((PASS++))
  else
    echo "  FAIL $desc"
    echo "       got:  $actual"
    echo "       want: $expected"
    ((FAIL++))
  fi
}

assert_notempty() {
  local desc="$1" actual="$2"
  if [ -n "$actual" ] && [ "$actual" != "null" ]; then
    echo "  PASS $desc"
    ((PASS++))
  else
    echo "  FAIL $desc (empty or null)"
    ((FAIL++))
  fi
}

echo ""
echo "=== agent-mesh V1 REST smoke test ==="
echo "Target: $BASE"
echo ""

# ── Projects ──────────────────────────────────────────────────────────────────
echo "--- Projects ---"
PROJECT=$(curl -sf -X POST "$BASE/api/projects" \
  -H "Content-Type: application/json" \
  -d '{"name":"smoke-test","repo_url":"https://github.com/test/repo"}')
PROJECT_ID=$(echo "$PROJECT" | jq -r .id)
assert "create project: name" "$(echo "$PROJECT" | jq -r .name)" "smoke-test"
assert_notempty "create project: id" "$PROJECT_ID"

GET_PROJECT=$(curl -sf "$BASE/api/projects/$PROJECT_ID")
assert "get project" "$(echo "$GET_PROJECT" | jq -r .id)" "$PROJECT_ID"

# ── Tasks ─────────────────────────────────────────────────────────────────────
echo ""
echo "--- Tasks ---"
TASK=$(curl -sf -X POST "$BASE/api/tasks" \
  -H "Content-Type: application/json" \
  -d "{\"project_id\":\"$PROJECT_ID\",\"title\":\"Add login endpoint\",
       \"description\":\"Implement POST /auth/login returning a JWT\",
       \"priority\":\"high\",\"branch_type\":\"feature\",\"created_by\":\"human\"}")
TASK_ID=$(echo "$TASK" | jq -r .id)
assert "create task: status" "$(echo "$TASK" | jq -r .status)" "backlog"
assert_notempty "create task: id" "$TASK_ID"
assert_notempty "create task: branch_name" "$(echo "$TASK" | jq -r .branch_name)"

GET_TASK=$(curl -sf "$BASE/api/tasks/$TASK_ID")
assert "get task" "$(echo "$GET_TASK" | jq -r .id)" "$TASK_ID"

# Move backlog → ready
curl -sf -X PATCH "$BASE/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from":"backlog","status_to":"ready"}' > /dev/null
assert "move task to ready" "$(curl -sf "$BASE/api/tasks/$TASK_ID" | jq -r .status)" "ready"

# List tasks
LIST=$(curl -sf "$BASE/api/tasks?project_id=$PROJECT_ID")
assert "list tasks: count" "$(echo "$LIST" | jq 'length')" "1"
assert "list tasks: status" "$(echo "$LIST" | jq -r '.[0].status')" "ready"

# ── Agents ────────────────────────────────────────────────────────────────────
echo ""
echo "--- Agents ---"
AGENT=$(curl -sf -X POST "$BASE/api/agents/register" \
  -H "Content-Type: application/json" \
  -d "{\"project_id\":\"$PROJECT_ID\",\"role\":\"coder\",
       \"name\":\"test-coder\",\"model\":\"stub\"}")
AGENT_ID=$(echo "$AGENT" | jq -r .id)
assert "register agent: role" "$(echo "$AGENT" | jq -r .role)" "coder"
assert_notempty "register agent: id" "$AGENT_ID"

AGENTS=$(curl -sf "$BASE/api/agents?project_id=$PROJECT_ID")
assert "list agents: count" "$(echo "$AGENTS" | jq 'length')" "1"

GET_AGENT=$(curl -sf "$BASE/api/agents/$AGENT_ID")
assert "get agent: role" "$(echo "$GET_AGENT" | jq -r .role)" "coder"

# ── Threads ───────────────────────────────────────────────────────────────────
echo ""
echo "--- Threads ---"
THREAD_LIST=$(curl -sf "$BASE/api/threads?task_id=$TASK_ID")
assert "auto-created task thread" "$(echo "$THREAD_LIST" | jq 'length')" "1"
THREAD_ID=$(echo "$THREAD_LIST" | jq -r '.[0].id')
assert_notempty "thread id" "$THREAD_ID"
assert "thread type" "$(echo "$THREAD_LIST" | jq -r '.[0].type')" "task"

MSG=$(curl -sf -X POST "$BASE/api/threads/$THREAD_ID/messages" \
  -H "Content-Type: application/json" \
  -d "{\"agent_id\":\"$AGENT_ID\",\"post_type\":\"progress\",
       \"content\":\"Starting implementation of login endpoint\"}")
MSG_ID=$(echo "$MSG" | jq -r .id)
assert "post message: post_type" "$(echo "$MSG" | jq -r .post_type)" "progress"
assert_notempty "post message: id" "$MSG_ID"

MSGS=$(curl -sf "$BASE/api/threads/$THREAD_ID/messages")
assert "list messages: count" "$(echo "$MSGS" | jq 'length')" "1"
assert "list messages: content" "$(echo "$MSGS" | jq -r '.[0].post_type')" "progress"

# ── Prompts ───────────────────────────────────────────────────────────────────
echo ""
echo "--- Prompts ---"
# GET global default (seeded in migration)
DEFAULT_PROMPT=$(curl -sf "$BASE/api/projects/$PROJECT_ID/prompts/coder")
assert_notempty "get default coder prompt: role" "$(echo "$DEFAULT_PROMPT" | jq -r .role)"

# SET project-specific override
curl -sf -X PUT "$BASE/api/projects/$PROJECT_ID/prompts/coder" \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a Go engineer. Always write table-driven tests."}' > /dev/null
PROJ_PROMPT=$(curl -sf "$BASE/api/projects/$PROJECT_ID/prompts/coder")
assert "set project prompt: content" "$(echo "$PROJ_PROMPT" | jq -r .content)" \
  "You are a Go engineer. Always write table-driven tests."

# ── Status transitions via REST ───────────────────────────────────────────────
echo ""
echo "--- Status transitions ---"
# ready → in_qa (simulate coder advancing via REST for testing)
curl -sf -X PATCH "$BASE/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from":"ready","status_to":"in_progress"}' > /dev/null
assert "ready → in_progress" "$(curl -sf "$BASE/api/tasks/$TASK_ID" | jq -r .status)" "in_progress"

curl -sf -X PATCH "$BASE/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from":"in_progress","status_to":"in_qa"}' > /dev/null
assert "in_progress → in_qa" "$(curl -sf "$BASE/api/tasks/$TASK_ID" | jq -r .status)" "in_qa"

curl -sf -X PATCH "$BASE/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from":"in_qa","status_to":"in_review"}' > /dev/null
assert "in_qa → in_review" "$(curl -sf "$BASE/api/tasks/$TASK_ID" | jq -r .status)" "in_review"

curl -sf -X PATCH "$BASE/api/tasks/$TASK_ID" \
  -H "Content-Type: application/json" \
  -d '{"status_from":"in_review","status_to":"merged"}' > /dev/null
assert "in_review → merged" "$(curl -sf "$BASE/api/tasks/$TASK_ID" | jq -r .status)" "merged"

# ── Dependencies ──────────────────────────────────────────────────────────────
echo ""
echo "--- Dependencies ---"
TASK2=$(curl -sf -X POST "$BASE/api/tasks" \
  -H "Content-Type: application/json" \
  -d "{\"project_id\":\"$PROJECT_ID\",\"title\":\"Write tests\",
       \"description\":\"Add unit tests for login\",\"priority\":\"medium\",
       \"branch_type\":\"feature\",\"created_by\":\"human\"}")
TASK2_ID=$(echo "$TASK2" | jq -r .id)
curl -sf -X POST "$BASE/api/tasks/$TASK2_ID/dependencies" \
  -H "Content-Type: application/json" \
  -d "{\"depends_on_id\":\"$TASK_ID\"}" > /dev/null
assert_notempty "add dependency" "$TASK2_ID"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "======================================="
echo "Results: $PASS passed, $FAIL failed"
echo "======================================="
[ $FAIL -eq 0 ] && exit 0 || exit 1
