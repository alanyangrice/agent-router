"""
Example agent that demonstrates the SDK contract.

This agent:
1. Registers with the server
2. Polls for assigned tasks
3. For each task: reads codebase, makes a simple change, commits, pushes, opens a PR
4. Posts progress updates to the task thread

This is NOT a production agent — it proves the SDK and server integration work end-to-end.
"""

import logging
import time
import sys
import os
from uuid import UUID
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "agent-sdk"))

from agent_sdk.base import BaseAgent
from agent_sdk.types import TaskStatus, PostType
from agent_sdk.local_tools import LocalTools
from agent_sdk.code_nav import CodeNavigator

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(levelname)s %(message)s")
logger = logging.getLogger("example-agent")


class ExampleAgent(BaseAgent):
    def __init__(self, server_url: str, project_id: UUID, repo_path: str):
        super().__init__(
            server_url=server_url,
            project_id=project_id,
            role="coder",
            name="Example Coder",
            model="example",
            skills=["go", "python"],
        )
        self.tools = LocalTools(repo_path)
        self.nav = CodeNavigator(repo_path)
        self.repo_path = repo_path

    def run(self):
        logger.info("Example agent running, polling for tasks...")

        while self._running:
            try:
                tasks = self.client.list_tasks(
                    self.project_id, status=TaskStatus.IN_PROGRESS
                )

                my_tasks = [t for t in tasks if t.assigned_agent_id == self.agent_id]

                if not my_tasks:
                    time.sleep(5)
                    continue

                for task in my_tasks:
                    self._work_on_task(task)

            except KeyboardInterrupt:
                break
            except Exception as e:
                logger.error(f"Error in agent loop: {e}")
                time.sleep(10)

    def _work_on_task(self, task):
        logger.info(f"Working on task: {task.title} ({task.id})")

        threads = self.client._http.get(
            "/api/threads/", params={"task_id": str(task.id)}
        )
        thread_list = threads.json()
        if not thread_list:
            logger.warning(f"No thread found for task {task.id}")
            return
        thread_id = UUID(thread_list[0]["id"])

        self.post_update(thread_id, f"Starting work on: {task.title}")

        tree = self.nav.get_tree(".", depth=2)
        self.post_update(thread_id, f"Explored codebase structure:\n```\n{tree}\n```")

        branch = task.branch_name
        output, code = self.tools.git_branch(branch)
        if code != 0 and "already exists" not in output:
            self.post_update(thread_id, f"Branch creation issue: {output}", PostType.BLOCKER)
            return

        readme_path = "README.md"
        try:
            content = self.tools.read_file(readme_path)
            content += f"\n\n<!-- Task: {task.title} (ID: {task.id}) -->\n"
            self.tools.write_file(readme_path, content)
        except FileNotFoundError:
            self.tools.write_file(readme_path, f"# Project\n\nTask: {task.title}\n")

        self.post_update(thread_id, "Made changes, running commit...")

        output, code = self.tools.git_commit(f"feat: {task.title}")
        if code != 0:
            self.post_update(thread_id, f"Commit failed: {output}", PostType.BLOCKER)
            return

        self.post_update(thread_id, "Pushing branch...")
        output, code = self.tools.git_push(branch)
        if code != 0:
            self.post_update(thread_id, f"Push failed: {output}", PostType.BLOCKER)
            return

        self.post_update(thread_id, "Opening PR...")
        try:
            pr = self.client.open_pr(
                title=f"feat: {task.title}",
                body=f"Automated PR for task {task.id}\n\n{task.description}",
                head=branch,
                base="main",
            )
            self.post_update(
                thread_id,
                f"PR opened: {pr.url}",
                PostType.ARTIFACT,
            )
        except Exception as e:
            self.post_update(thread_id, f"PR creation failed: {e}", PostType.BLOCKER)
            return

        try:
            self.client.update_task_status(task.id, TaskStatus.IN_PROGRESS, TaskStatus.IN_REVIEW)
            self.post_update(thread_id, "Task moved to review.")
        except Exception as e:
            logger.error(f"Failed to update task status: {e}")

        logger.info(f"Completed work on task: {task.title}")


def main():
    server_url = os.environ.get("AGENT_MESH_URL", "http://localhost:8080")
    project_id = os.environ.get("AGENT_MESH_PROJECT_ID")
    repo_path = os.environ.get("AGENT_MESH_REPO_PATH", ".")

    if not project_id:
        print("Error: AGENT_MESH_PROJECT_ID environment variable is required")
        sys.exit(1)

    agent = ExampleAgent(server_url, UUID(project_id), repo_path)
    agent.start()


if __name__ == "__main__":
    main()
