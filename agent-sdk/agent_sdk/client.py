import httpx
from uuid import UUID, uuid4
from typing import Optional
from agent_sdk.types import Task, Thread, Message, Agent, PR, TaskStatus, PostType


class AgentMeshClient:
    def __init__(self, base_url: str = "http://localhost:8080"):
        self._base_url = base_url.rstrip("/")
        self._http = httpx.Client(base_url=self._base_url, timeout=30.0)

    # Agent lifecycle

    def register(self, project_id: UUID, role: str, name: str, model: str, skills: list[str]) -> Agent:
        resp = self._http.post("/api/agents/register", json={
            "project_id": str(project_id),
            "role": role,
            "name": name,
            "model": model,
            "skills": skills,
        })
        resp.raise_for_status()
        return Agent(**resp.json())

    def heartbeat(self, agent_id: UUID) -> None:
        resp = self._http.post(f"/api/agents/{agent_id}/heartbeat")
        resp.raise_for_status()

    # Tasks

    def list_tasks(
        self,
        project_id: UUID,
        status: Optional[TaskStatus] = None,
        assigned_to: Optional[UUID] = None,
    ) -> list[Task]:
        params: dict[str, str] = {"project_id": str(project_id)}
        if status:
            params["status"] = status.value
        if assigned_to:
            params["assigned_to"] = str(assigned_to)
        resp = self._http.get("/api/tasks/", params=params)
        resp.raise_for_status()
        return [Task(**t) for t in resp.json()]

    def get_task(self, task_id: UUID) -> Task:
        resp = self._http.get(f"/api/tasks/{task_id}")
        resp.raise_for_status()
        return Task(**resp.json())

    def update_task_status(self, task_id: UUID, from_status: TaskStatus, to_status: TaskStatus) -> None:
        resp = self._http.patch(f"/api/tasks/{task_id}", json={
            "status_from": from_status.value,
            "status_to": to_status.value,
        }, headers={"X-Idempotency-Key": str(uuid4())})
        resp.raise_for_status()

    # Threads & Messages

    def list_messages(self, thread_id: UUID) -> list[Message]:
        resp = self._http.get(f"/api/threads/{thread_id}/messages")
        resp.raise_for_status()
        return [Message(**m) for m in resp.json()]

    def post_message(self, thread_id: UUID, agent_id: UUID, post_type: PostType, content: str) -> Message:
        resp = self._http.post(f"/api/threads/{thread_id}/messages", json={
            "agent_id": str(agent_id),
            "post_type": post_type.value,
            "content": content,
        }, headers={"X-Idempotency-Key": str(uuid4())})
        resp.raise_for_status()
        return Message(**resp.json())

    # Git (server-mediated)

    def open_pr(self, title: str, body: str, head: str, base: str = "main") -> PR:
        resp = self._http.post("/api/git/pr", json={
            "title": title, "body": body, "head": head, "base": base,
        }, headers={"X-Idempotency-Key": str(uuid4())})
        resp.raise_for_status()
        return PR(**resp.json())

    def close(self):
        self._http.close()
