from enum import Enum
from datetime import datetime
from uuid import UUID
from pydantic import BaseModel, Field
from typing import Optional


class TaskStatus(str, Enum):
    BACKLOG = "backlog"
    READY = "ready"
    IN_PROGRESS = "in_progress"
    IN_REVIEW = "in_review"
    DONE = "done"


class Priority(str, Enum):
    CRITICAL = "critical"
    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"


class BranchType(str, Enum):
    FEATURE = "feature"
    FIX = "fix"
    REFACTOR = "refactor"


class PostType(str, Enum):
    PROGRESS = "progress"
    BLOCKER = "blocker"
    HELP_WANTED = "help_wanted"
    DECISION = "decision"
    ARTIFACT = "artifact"
    REVIEW_REQ = "review_req"
    COMMENT = "comment"


class Task(BaseModel):
    id: UUID
    project_id: UUID
    title: str
    description: str
    status: TaskStatus
    priority: Priority
    assigned_agent_id: Optional[UUID] = None
    parent_task_id: Optional[UUID] = None
    branch_type: BranchType
    branch_name: str
    labels: list[str] = Field(default_factory=list)
    created_by: str
    created_at: datetime
    updated_at: datetime
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None


class Thread(BaseModel):
    id: UUID
    project_id: UUID
    task_id: Optional[UUID] = None
    type: str
    name: str
    created_at: datetime


class Message(BaseModel):
    id: UUID
    thread_id: UUID
    agent_id: Optional[UUID] = None
    post_type: PostType
    content: str
    metadata: dict = Field(default_factory=dict)
    created_at: datetime


class Agent(BaseModel):
    id: UUID
    project_id: UUID
    role: str
    name: str
    skills: list[str] = Field(default_factory=list)
    model: str
    status: str
    current_task_id: Optional[UUID] = None
    created_at: datetime


class PR(BaseModel):
    id: int
    url: str
    number: int
    head: str
    base: str
    title: str
    body: str
