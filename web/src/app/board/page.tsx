"use client";

import { useEffect, useState, useCallback } from "react";
import { api, Task, Agent } from "@/lib/api";
import { useWebSocket } from "@/hooks/useWebSocket";
import { DashboardShell } from "@/components/DashboardShell";
import { TaskCard } from "@/components/TaskCard";

const COLUMNS: { key: Task["status"]; label: string; color: string }[] = [
  { key: "backlog", label: "Backlog", color: "bg-gray-500" },
  { key: "ready", label: "Ready", color: "bg-purple-500" },
  { key: "in_progress", label: "In Progress", color: "bg-blue-500" },
  { key: "in_review", label: "In Review", color: "bg-yellow-500" },
  { key: "done", label: "Done", color: "bg-green-500" },
];

export default function BoardPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreateForm, setShowCreateForm] = useState(false);

  const fetchData = useCallback(async () => {
    try {
      const [taskList, agentList] = await Promise.all([
        api.listTasks(),
        api.listAgents(),
      ]);
      setTasks(taskList);
      setAgents(agentList);
    } catch {
      /* API not available yet */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useWebSocket((event) => {
    if (event.type.startsWith("task") || event.type.startsWith("agent")) {
      fetchData();
    }
  });

  const agentMap = new Map(agents.map((a) => [a.id, a]));

  const tasksByStatus = (status: Task["status"]) =>
    tasks.filter((t) => t.status === status);

  return (
    <DashboardShell>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-white">Task Board</h1>
          <p className="mt-1 text-sm text-gray-400">
            {tasks.length} task{tasks.length !== 1 && "s"} across{" "}
            {agents.length} agent{agents.length !== 1 && "s"}
          </p>
        </div>
        <button
          onClick={() => setShowCreateForm(true)}
          className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-500"
        >
          + New Task
        </button>
      </div>

      {showCreateForm && (
        <CreateTaskForm
          onCreated={() => {
            setShowCreateForm(false);
            fetchData();
          }}
          onCancel={() => setShowCreateForm(false)}
        />
      )}

      {loading ? (
        <div className="flex h-96 items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" />
        </div>
      ) : (
        <div className="grid grid-cols-5 gap-4">
          {COLUMNS.map((col) => {
            const colTasks = tasksByStatus(col.key);
            return (
              <div key={col.key} className="flex flex-col">
                <div className="mb-3 flex items-center gap-2">
                  <span
                    className={`h-2.5 w-2.5 rounded-full ${col.color}`}
                  />
                  <h2 className="text-sm font-medium text-gray-300">
                    {col.label}
                  </h2>
                  <span className="ml-auto text-xs text-gray-500">
                    {colTasks.length}
                  </span>
                </div>

                <div className="flex-1 space-y-2 rounded-lg border border-gray-800 bg-gray-900/50 p-2 min-h-[200px]">
                  {colTasks.length === 0 ? (
                    <div className="flex h-24 items-center justify-center text-xs text-gray-600">
                      No tasks
                    </div>
                  ) : (
                    colTasks.map((task) => (
                      <TaskCard
                        key={task.id}
                        task={task}
                        agentName={
                          task.assigned_agent_id
                            ? agentMap.get(task.assigned_agent_id)?.name
                            : undefined
                        }
                      />
                    ))
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </DashboardShell>
  );
}

function CreateTaskForm({
  onCreated,
  onCancel,
}: {
  onCreated: () => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState("medium");
  const [branchType, setBranchType] = useState("feature");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim()) return;

    setSubmitting(true);
    setError("");

    const projectId = localStorage.getItem("agent_mesh_project_id");
    if (!projectId) {
      setError("No project selected. Go to setup first.");
      setSubmitting(false);
      return;
    }

    try {
      await api.createTask({
        project_id: projectId,
        title: title.trim(),
        description: description.trim(),
        priority: priority as Task["priority"],
        branch_type: branchType,
        created_by: "human",
      });
      onCreated();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create task");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mb-6 rounded-xl border border-gray-800 bg-gray-800/50 p-5">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-base font-medium text-white">New Task</h2>
        <button
          onClick={onCancel}
          className="text-sm text-gray-400 hover:text-gray-200"
        >
          Cancel
        </button>
      </div>

      <form onSubmit={handleSubmit} className="space-y-4">
        <div>
          <label className="mb-1 block text-sm text-gray-400">Title</label>
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="What needs to be done?"
            className="w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white placeholder-gray-500 outline-none focus:border-blue-500"
            required
          />
        </div>

        <div>
          <label className="mb-1 block text-sm text-gray-400">
            Description
          </label>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Details, acceptance criteria, context..."
            rows={3}
            className="w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white placeholder-gray-500 outline-none focus:border-blue-500"
          />
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="mb-1 block text-sm text-gray-400">
              Priority
            </label>
            <select
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              className="w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white outline-none focus:border-blue-500"
            >
              <option value="critical">Critical</option>
              <option value="high">High</option>
              <option value="medium">Medium</option>
              <option value="low">Low</option>
            </select>
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">
              Branch Type
            </label>
            <select
              value={branchType}
              onChange={(e) => setBranchType(e.target.value)}
              className="w-full rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white outline-none focus:border-blue-500"
            >
              <option value="feature">Feature</option>
              <option value="fix">Fix</option>
              <option value="refactor">Refactor</option>
            </select>
          </div>
        </div>

        {error && (
          <div className="rounded-lg bg-red-900/30 px-3 py-2 text-sm text-red-400">
            {error}
          </div>
        )}

        <button
          type="submit"
          disabled={submitting}
          className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-500 disabled:opacity-50"
        >
          {submitting ? "Creating..." : "Create Task"}
        </button>
      </form>
    </div>
  );
}
