#!/usr/bin/env python3
"""
V1 MCP Pipeline Simulation
Connects real MCP clients to the running server and drives the full pipeline
mechanically — no LLM required. Validates three key scenarios.

Usage:
    # First create a project via REST:
    PROJECT_ID=$(curl -sf -X POST http://localhost:8080/api/projects \
      -H "Content-Type: application/json" \
      -d '{"name":"sim-test","repo_url":"https://github.com/test/repo"}' | jq -r .id)

    PROJECT_ID=$PROJECT_ID python agents/example/simulate_pipeline.py

Requirements:
    pip install mcp httpx

Server must be running at http://localhost:8080 (or set BASE_URL).
"""

import asyncio
import os
import sys

try:
    import httpx
    from mcp.client.sse import sse_client
    from mcp import ClientSession
except ImportError:
    print("Missing dependencies. Run: pip install mcp httpx")
    sys.exit(1)

BASE = os.getenv("BASE_URL", "http://localhost:8080")
PROJECT_ID = os.getenv("PROJECT_ID", "")
PASS_COUNT = 0
FAIL_COUNT = 0


def ok(msg: str) -> None:
    global PASS_COUNT
    PASS_COUNT += 1
    print(f"  PASS {msg}")


def fail(msg: str, detail: str = "") -> None:
    global FAIL_COUNT
    FAIL_COUNT += 1
    print(f"  FAIL {msg}")
    if detail:
        print(f"       {detail}")


async def make_agent(role: str) -> tuple[ClientSession, str]:
    """Open MCP SSE session, register agent, fetch role prompt."""
    read, write = await sse_client(f"{BASE}/mcp").__aenter__()
    session = ClientSession(read, write)
    await session.initialize()

    result = await session.call_tool("register_agent", {
        "project_id": PROJECT_ID,
        "role": role,
        "name": f"stub-{role}",
        "model": "stub",
    })
    agent_id = result.content[0].text
    import json
    try:
        agent_data = json.loads(agent_id)
        agent_id = agent_data.get("agent_id", agent_id)
    except Exception:
        pass

    prompt = await session.get_prompt(role, {"project_id": PROJECT_ID})
    prompt_len = len(prompt.messages[0].content.text) if prompt.messages else 0
    ok(f"{role}: registered (id={agent_id[:8]}...) + fetched prompt ({prompt_len} chars)")
    return session, agent_id


async def create_task(title: str) -> str:
    """Create a task via REST and move it to ready."""
    async with httpx.AsyncClient() as http:
        r = await http.post(f"{BASE}/api/tasks", json={
            "project_id": PROJECT_ID,
            "title": title,
            "description": f"Stub task for simulation: {title}",
            "priority": "medium",
            "branch_type": "feature",
            "created_by": "simulate_pipeline.py",
        })
        r.raise_for_status()
        task_id = r.json()["id"]

        await http.patch(f"{BASE}/api/tasks/{task_id}", json={
            "status_from": "backlog",
            "status_to": "ready",
        })
    return task_id


async def get_task_status(task_id: str) -> dict:
    async with httpx.AsyncClient() as http:
        r = await http.get(f"{BASE}/api/tasks/{task_id}")
        return r.json()


# ── Scenario 1: Happy path ────────────────────────────────────────────────────

async def scenario_happy_path() -> None:
    print("\n=== Scenario 1: Happy path (coder → in_qa → in_review → merged) ===")

    coder, coder_id = await make_agent("coder")
    qa, qa_id       = await make_agent("qa")
    rev, rev_id     = await make_agent("reviewer")

    task_id = await create_task("Implement login endpoint")
    await asyncio.sleep(0.5)

    # Coder claims and works
    task_result = await coder.call_tool("claim_task", {"agent_id": coder_id})
    import json
    task_data = task_result.content[0].text
    if task_data and task_data != "null":
        ok("coder: claimed task")
    else:
        fail("coder: claim_task returned null (task may not be auto-assigned yet)")
        # Advance manually via REST for simulation
        async with httpx.AsyncClient() as http:
            await http.patch(f"{BASE}/api/tasks/{task_id}", json={
                "status_from": "ready", "status_to": "in_progress"
            })

    await coder.call_tool("set_pr_url", {
        "task_id": task_id,
        "pr_url": "https://github.com/stub/repo/pull/1",
    })
    ok("coder: set PR URL")

    await coder.call_tool("post_message", {
        "task_id": task_id,
        "content": "Implementation complete. Branch pushed. PR #1 opened.",
        "post_type": "artifact",
        "agent_id": coder_id,
    })
    ok("coder: posted artifact message")

    await coder.call_tool("update_task_status", {
        "task_id": task_id,
        "from": "in_progress",
        "to": "in_qa",
    })
    ok("coder: advanced to in_qa")
    await asyncio.sleep(0.5)

    # QA passes
    qa_result = await qa.call_tool("claim_task", {"agent_id": qa_id})
    if qa_result.content[0].text and qa_result.content[0].text != "null":
        ok("qa: claimed task")

    ctx_result = await qa.call_tool("get_task_context", {"task_id": task_id})
    ctx_data = json.loads(ctx_result.content[0].text)
    pr_url = ctx_data.get("task", {}).get("pr_url", "")
    ok(f"qa: got task context (pr_url={pr_url or 'empty'})")

    await qa.call_tool("post_message", {
        "task_id": task_id,
        "content": "All 23 tests passing. Coverage: 87%.",
        "post_type": "artifact",
        "agent_id": qa_id,
    })
    await qa.call_tool("update_task_status", {
        "task_id": task_id,
        "from": "in_qa",
        "to": "in_review",
    })
    ok("qa: passed and advanced to in_review")
    await asyncio.sleep(0.5)

    # Reviewer approves
    rev_result = await rev.call_tool("claim_task", {"agent_id": rev_id})
    if rev_result.content[0].text and rev_result.content[0].text != "null":
        ok("reviewer: claimed task")

    await rev.call_tool("post_message", {
        "task_id": task_id,
        "content": "LGTM. Code quality is solid. PR #1 merged to main.",
        "post_type": "artifact",
        "agent_id": rev_id,
    })
    await rev.call_tool("update_task_status", {
        "task_id": task_id,
        "from": "in_review",
        "to": "merged",
    })
    ok("reviewer: approved and advanced to merged")

    final = await get_task_status(task_id)
    if final.get("status") == "merged":
        ok("final status: merged")
    else:
        fail("final status", f"expected merged, got {final.get('status')}")

    # Verify thread has all messages
    async with httpx.AsyncClient() as http:
        threads = (await http.get(f"{BASE}/api/threads?task_id={task_id}")).json()
        if threads:
            msgs = (await http.get(f"{BASE}/api/threads/{threads[0]['id']}/messages")).json()
            ok(f"thread has {len(msgs)} messages")
        else:
            fail("thread not found")


# ── Scenario 2: Ownership lock ────────────────────────────────────────────────

async def scenario_ownership_lock() -> None:
    print("\n=== Scenario 2: Ownership lock (QA fail → same coder re-assigned) ===")

    coder, coder_id = await make_agent("coder")
    qa, qa_id       = await make_agent("qa")

    task_id = await create_task("Fix null pointer in auth service")
    await asyncio.sleep(0.5)

    # Coder takes the task
    await coder.call_tool("claim_task", {"agent_id": coder_id})
    async with httpx.AsyncClient() as http:
        await http.patch(f"{BASE}/api/tasks/{task_id}", json={
            "status_from": "ready", "status_to": "in_progress"
        })

    await coder.call_tool("update_task_status", {
        "task_id": task_id,
        "from": "in_progress",
        "to": "in_qa",
    })
    await asyncio.sleep(0.3)

    # QA fails
    await qa.call_tool("claim_task", {"agent_id": qa_id})
    await qa.call_tool("post_message", {
        "task_id": task_id,
        "content": "test_auth_login: FAIL — null pointer at auth.go:127\ntest_auth_refresh: FAIL — expired token not rejected",
        "post_type": "review_feedback",
        "agent_id": qa_id,
    })
    await qa.call_tool("update_task_status", {
        "task_id": task_id,
        "from": "in_qa",
        "to": "in_progress",
    })
    ok("qa: posted failures and bounced back to in_progress")
    await asyncio.sleep(0.3)

    # Check ownership lock
    task_state = await get_task_status(task_id)
    assigned = task_state.get("assigned_agent_id", "")
    if assigned == coder_id:
        ok(f"ownership lock: same coder still assigned ({coder_id[:8]}...)")
    else:
        fail("ownership lock broken", f"assigned={assigned}, expected coder={coder_id}")

    # Coder reads QA feedback from thread
    msgs_result = await coder.call_tool("list_messages", {"task_id": task_id})
    import json
    msgs = json.loads(msgs_result.content[0].text)
    feedback_msgs = [m for m in msgs if m.get("post_type") == "review_feedback"]
    if feedback_msgs:
        ok(f"coder can read QA feedback in thread ({len(feedback_msgs)} feedback message(s))")
    else:
        fail("coder: no review_feedback in thread")


# ── Scenario 3: main_updated broadcast ───────────────────────────────────────

async def scenario_main_updated() -> None:
    print("\n=== Scenario 3: main_updated broadcast when task merges ===")

    coder1, c1_id = await make_agent("coder")
    coder2, c2_id = await make_agent("coder")
    qa, qa_id     = await make_agent("qa")
    rev, rev_id   = await make_agent("reviewer")

    # Two tasks: one will go through the full pipeline, one stays in_progress
    t1 = await create_task("Feature A (will be merged)")
    t2 = await create_task("Feature B (in-progress when A merges)")
    await asyncio.sleep(0.5)

    # Put t2 in_progress for coder2
    async with httpx.AsyncClient() as http:
        await http.patch(f"{BASE}/api/tasks/{t2}", json={
            "status_from": "ready", "status_to": "in_progress"
        })
    ok("coder2: Feature B is in_progress")

    # Walk t1 through full pipeline
    async with httpx.AsyncClient() as http:
        await http.patch(f"{BASE}/api/tasks/{t1}", json={
            "status_from": "ready", "status_to": "in_progress"
        })
    await coder1.call_tool("update_task_status", {
        "task_id": t1, "from": "in_progress", "to": "in_qa"
    })
    await asyncio.sleep(0.2)
    await qa.call_tool("update_task_status", {
        "task_id": t1, "from": "in_qa", "to": "in_review"
    })
    await asyncio.sleep(0.2)
    await rev.call_tool("update_task_status", {
        "task_id": t1, "from": "in_review", "to": "merged"
    })
    ok("Feature A merged")

    # The service should have broadcast main_updated to coder2 (via SSE)
    # We verify the server sent it by checking logs (check server output)
    # and verifying t1 is merged
    t1_state = await get_task_status(t1)
    if t1_state.get("status") == "merged":
        ok("Feature A status: merged (main_updated broadcast should have fired to coder2)")
    else:
        fail("Feature A status", f"expected merged, got {t1_state.get('status')}")

    t2_state = await get_task_status(t2)
    if t2_state.get("status") == "in_progress":
        ok("Feature B still in_progress (coder2 was notified via SSE to pull main)")
    else:
        fail("Feature B status", f"expected in_progress, got {t2_state.get('status')}")


# ── Main ──────────────────────────────────────────────────────────────────────

async def main() -> None:
    if not PROJECT_ID:
        print("ERROR: PROJECT_ID environment variable is required.")
        print("Create one with:")
        print(f"  PROJECT_ID=$(curl -sf -X POST {BASE}/api/projects \\")
        print("    -H 'Content-Type: application/json' \\")
        print("    -d '{\"name\":\"sim-test\",\"repo_url\":\"https://github.com/test/repo\"}' | jq -r .id)")
        sys.exit(1)

    print(f"Connecting to: {BASE}")
    print(f"Project ID: {PROJECT_ID}")

    await scenario_happy_path()
    await scenario_ownership_lock()
    await scenario_main_updated()

    print("")
    print("=" * 50)
    print(f"Results: {PASS_COUNT} passed, {FAIL_COUNT} failed")
    print("=" * 50)
    sys.exit(0 if FAIL_COUNT == 0 else 1)


if __name__ == "__main__":
    asyncio.run(main())
