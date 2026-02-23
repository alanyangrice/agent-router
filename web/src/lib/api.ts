const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

async function fetchAPI<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
  });
  if (!res.ok) {
    const error = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(error.error || res.statusText);
  }
  return res.json();
}

export interface Task {
  id: string;
  project_id: string;
  title: string;
  description: string;
  status: "backlog" | "ready" | "in_progress" | "in_qa" | "in_review" | "merged";
  priority: "critical" | "high" | "medium" | "low";
  assigned_agent_id?: string;
  branch_type: string;
  branch_name: string;
  labels: string[];
  required_role?: string;
  pr_url?: string;
  created_by: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  completed_at?: string;
}

export interface Thread {
  id: string;
  project_id: string;
  task_id?: string;
  type: string;
  name: string;
  created_at: string;
}

export interface Message {
  id: string;
  thread_id: string;
  agent_id?: string;
  post_type: "progress" | "review_feedback" | "blocker" | "artifact" | "comment";
  content: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface Agent {
  id: string;
  project_id: string;
  role: string;
  name: string;
  skills: string[];
  model: string;
  status: "idle" | "working" | "blocked" | "offline";
  current_task_id?: string;
  last_heartbeat_at?: string;
  created_at: string;
}

export interface Project {
  id: string;
  name: string;
  repo_url: string;
  created_at: string;
}

export interface RolePrompt {
  id: string;
  project_id?: string;
  role: string;
  content: string;
  created_at: string;
}

export const api = {
  createProject: (data: { name: string; repo_url: string }) =>
    fetchAPI<Project>("/api/projects/", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  getProject: (id: string) => fetchAPI<Project>(`/api/projects/${id}`),

  listTasks: (params?: Record<string, string>) => {
    const query = params ? "?" + new URLSearchParams(params).toString() : "";
    return fetchAPI<Task[]>(`/api/tasks/${query}`);
  },
  getTask: (id: string) => fetchAPI<Task>(`/api/tasks/${id}`),
  createTask: (data: Partial<Task> & { branch_type?: string }) =>
    fetchAPI<Task>("/api/tasks/", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  updateTaskStatus: (id: string, from: string, to: string) =>
    fetchAPI<Task>(`/api/tasks/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ status_from: from, status_to: to }),
    }),

  listThreads: (params?: Record<string, string>) => {
    const query = params ? "?" + new URLSearchParams(params).toString() : "";
    return fetchAPI<Thread[]>(`/api/threads/${query}`);
  },
  listMessages: (threadId: string) =>
    fetchAPI<Message[]>(`/api/threads/${threadId}/messages`),
  postMessage: (
    threadId: string,
    data: { agent_id?: string; post_type: string; content: string }
  ) =>
    fetchAPI<Message>(`/api/threads/${threadId}/messages`, {
      method: "POST",
      body: JSON.stringify(data),
    }),

  listAgents: (params?: Record<string, string>) => {
    const query = params ? "?" + new URLSearchParams(params).toString() : "";
    return fetchAPI<Agent[]>(`/api/agents/${query}`);
  },
  getAgent: (id: string) => fetchAPI<Agent>(`/api/agents/${id}`),

  // Role prompts
  getPrompt: (projectId: string, role: string) =>
    fetchAPI<RolePrompt>(`/api/projects/${projectId}/prompts/${role}`),
  setPrompt: (projectId: string, role: string, content: string) =>
    fetchAPI<RolePrompt>(`/api/projects/${projectId}/prompts/${role}`, {
      method: "PUT",
      body: JSON.stringify({ content }),
    }),
};
