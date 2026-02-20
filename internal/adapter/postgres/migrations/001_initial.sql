CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Projects
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    config_json JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Agents (created before tasks so tasks.assigned_agent_id FK is valid)
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id),
    role TEXT NOT NULL,
    name TEXT NOT NULL,
    skills TEXT[] DEFAULT '{}',
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'idle' CHECK (status IN ('idle', 'working', 'blocked', 'offline')),
    current_task_id UUID,
    config_jsonb JSONB DEFAULT '{}',
    stats_jsonb JSONB DEFAULT '{}',
    last_heartbeat_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tasks
CREATE TABLE tasks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'backlog' CHECK (status IN ('backlog', 'ready', 'in_progress', 'in_review', 'done')),
    priority TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('critical', 'high', 'medium', 'low')),
    assigned_agent_id UUID REFERENCES agents(id),
    parent_task_id UUID REFERENCES tasks(id),
    branch_type TEXT NOT NULL DEFAULT 'feature' CHECK (branch_type IN ('feature', 'fix', 'refactor')),
    branch_name TEXT NOT NULL DEFAULT '',
    labels TEXT[] DEFAULT '{}',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

-- Now that tasks exists, add the FK from agents.current_task_id -> tasks.id
ALTER TABLE agents ADD CONSTRAINT fk_agents_current_task FOREIGN KEY (current_task_id) REFERENCES tasks(id);

CREATE TABLE task_dependencies (
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, depends_on_id),
    CHECK (task_id != depends_on_id)
);

-- Threads
CREATE TABLE threads (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id),
    task_id UUID REFERENCES tasks(id),
    type TEXT NOT NULL CHECK (type IN ('task', 'general', 'task_board', 'pr_merging', 'blockers', 'arch_decision', 'escalation')),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE messages (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id),
    post_type TEXT NOT NULL CHECK (post_type IN ('progress', 'blocker', 'help_wanted', 'decision', 'artifact', 'review_req', 'comment')),
    content TEXT NOT NULL,
    metadata_jsonb JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE thread_visibility (
    thread_id UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    agent_role TEXT NOT NULL,
    PRIMARY KEY (thread_id, agent_role)
);

-- Reviews
CREATE TABLE reviews (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id UUID NOT NULL REFERENCES tasks(id),
    reviewer_agent_id UUID NOT NULL REFERENCES agents(id),
    pr_url TEXT NOT NULL,
    verdict TEXT NOT NULL CHECK (verdict IN ('approve', 'request_changes', 'comment')),
    comments_jsonb JSONB DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Idempotency
CREATE TABLE processed_operations (
    idempotency_key TEXT PRIMARY KEY,
    agent_id UUID REFERENCES agents(id),
    operation_type TEXT NOT NULL,
    result_jsonb JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_tasks_project_status ON tasks(project_id, status);
CREATE INDEX idx_tasks_assigned_agent ON tasks(assigned_agent_id);
CREATE INDEX idx_threads_project ON threads(project_id);
CREATE INDEX idx_threads_task ON threads(task_id);
CREATE INDEX idx_messages_thread ON messages(thread_id);
CREATE INDEX idx_agents_project_status ON agents(project_id, status);
CREATE INDEX idx_agents_heartbeat ON agents(last_heartbeat_at);
CREATE INDEX idx_processed_ops_created ON processed_operations(created_at);
