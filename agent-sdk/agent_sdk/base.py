import abc
import threading
import time
import logging
from uuid import UUID
from agent_sdk.client import AgentMeshClient
from agent_sdk.types import Task, PostType

logger = logging.getLogger(__name__)


class BaseAgent(abc.ABC):
    def __init__(self, server_url: str, project_id: UUID, role: str, name: str, model: str, skills: list[str]):
        self.client = AgentMeshClient(server_url)
        self.project_id = project_id
        self.agent = self.client.register(project_id, role, name, model, skills)
        self.agent_id = self.agent.id
        self._heartbeat_interval = 30
        self._running = False
        self._heartbeat_thread: threading.Thread | None = None

    def start(self):
        self._running = True
        self._heartbeat_thread = threading.Thread(target=self._heartbeat_loop, daemon=True)
        self._heartbeat_thread.start()
        logger.info(f"Agent {self.agent.name} ({self.agent_id}) started")
        try:
            self.run()
        finally:
            self.stop()

    def stop(self):
        self._running = False
        self.client.close()
        logger.info(f"Agent {self.agent.name} stopped")

    def _heartbeat_loop(self):
        while self._running:
            try:
                self.client.heartbeat(self.agent_id)
            except Exception as e:
                logger.warning(f"Heartbeat failed: {e}")
            time.sleep(self._heartbeat_interval)

    @abc.abstractmethod
    def run(self):
        """Main agent loop. Override in subclass."""
        ...

    def post_update(self, thread_id: UUID, content: str, post_type: PostType = PostType.PROGRESS):
        return self.client.post_message(thread_id, self.agent_id, post_type, content)
