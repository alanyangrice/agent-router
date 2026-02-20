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

  const fetchData = useCallback(async () => {
    try {
      const [taskList, agentList] = await Promise.all([
        api.listTasks(),
        api.listAgents(),
      ]);
      setTasks(taskList);
      setAgents(agentList);
    } catch {
      /* API not available yet — show empty state */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useWebSocket((event) => {
    if (event.type.startsWith("task.") || event.type.startsWith("agent.")) {
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
      </div>

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
                  <span className={`h-2.5 w-2.5 rounded-full ${col.color}`} />
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
