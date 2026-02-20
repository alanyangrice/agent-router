"use client";

import { useEffect, useState, useCallback } from "react";
import { api, Agent, Task } from "@/lib/api";
import { useWebSocket } from "@/hooks/useWebSocket";
import { DashboardShell } from "@/components/DashboardShell";
import { StatusBadge } from "@/components/StatusBadge";

const STATUS_DOT: Record<string, string> = {
  idle: "bg-green-400",
  working: "bg-blue-400 animate-pulse",
  blocked: "bg-yellow-400",
  offline: "bg-red-400",
};

function timeSince(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const secs = Math.floor(diff / 1000);
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export default function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [tasks, setTasks] = useState<Map<string, Task>>(new Map());
  const [loading, setLoading] = useState(true);

  const fetchData = useCallback(async () => {
    try {
      const [agentList, taskList] = await Promise.all([
        api.listAgents(),
        api.listTasks(),
      ]);
      setAgents(agentList);
      setTasks(new Map(taskList.map((t) => [t.id, t])));
    } catch {
      /* API not available */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useWebSocket((event) => {
    if (event.type.startsWith("agent.") || event.type.startsWith("task.")) {
      fetchData();
    }
  });

  return (
    <DashboardShell>
      <div className="mb-6">
        <h1 className="text-xl font-semibold text-white">Agents</h1>
        <p className="mt-1 text-sm text-gray-400">
          {agents.length} agent{agents.length !== 1 && "s"} registered
        </p>
      </div>

      {loading ? (
        <div className="flex h-96 items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" />
        </div>
      ) : agents.length === 0 ? (
        <div className="flex h-64 items-center justify-center rounded-xl border border-gray-800 bg-gray-800/30">
          <p className="text-sm text-gray-500">
            No agents registered yet. Start the orchestrator to spin up agents.
          </p>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {agents.map((agent) => {
            const currentTask = agent.current_task_id
              ? tasks.get(agent.current_task_id)
              : null;

            return (
              <div
                key={agent.id}
                className="rounded-xl border border-gray-800 bg-gray-800/50 p-5"
              >
                <div className="mb-3 flex items-start justify-between">
                  <div className="flex items-center gap-3">
                    <div className="relative">
                      <div className="flex h-10 w-10 items-center justify-center rounded-full bg-gray-700 text-sm font-semibold text-gray-200">
                        {agent.name.charAt(0).toUpperCase()}
                      </div>
                      <span
                        className={`absolute -bottom-0.5 -right-0.5 h-3 w-3 rounded-full border-2 border-gray-800 ${
                          STATUS_DOT[agent.status] || "bg-gray-400"
                        }`}
                      />
                    </div>
                    <div>
                      <h3 className="text-sm font-medium text-gray-100">
                        {agent.name}
                      </h3>
                      <p className="text-xs text-gray-500">{agent.role}</p>
                    </div>
                  </div>
                  <StatusBadge status={agent.status} />
                </div>

                {currentTask && (
                  <div className="mb-3 rounded-lg bg-gray-900/50 px-3 py-2">
                    <p className="text-[10px] font-medium uppercase tracking-wider text-gray-500">
                      Current Task
                    </p>
                    <p className="mt-0.5 truncate text-sm text-gray-300">
                      {currentTask.title}
                    </p>
                  </div>
                )}

                <div className="mb-3 flex flex-wrap gap-1">
                  {agent.skills.map((skill) => (
                    <span
                      key={skill}
                      className="rounded bg-gray-700 px-1.5 py-0.5 text-[10px] text-gray-400"
                    >
                      {skill}
                    </span>
                  ))}
                </div>

                <div className="flex items-center justify-between border-t border-gray-700/50 pt-3 text-xs text-gray-500">
                  <span>Model: {agent.model}</span>
                  {agent.last_heartbeat_at && (
                    <span title={agent.last_heartbeat_at}>
                      Heartbeat: {timeSince(agent.last_heartbeat_at)}
                    </span>
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
