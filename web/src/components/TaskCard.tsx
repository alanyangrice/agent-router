"use client";

import { Task } from "@/lib/api";
import { PriorityBadge } from "./PriorityBadge";

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function isStuck(task: Task): boolean {
  if (task.status !== "in_progress" || !task.started_at) return false;
  const elapsed = Date.now() - new Date(task.started_at).getTime();
  return elapsed > 30 * 60 * 1000;
}

export function TaskCard({
  task,
  agentName,
}: {
  task: Task;
  agentName?: string;
}) {
  const stuck = isStuck(task);

  return (
    <div
      className={`group relative rounded-lg border p-3 transition-colors hover:border-gray-600 ${
        stuck
          ? "border-yellow-500/50 bg-yellow-900/10"
          : "border-gray-700 bg-gray-800"
      }`}
    >
      {stuck && (
        <div className="absolute -top-2 -right-2 flex h-5 w-5 items-center justify-center rounded-full bg-yellow-500 text-[10px] font-bold text-gray-900">
          !
        </div>
      )}

      <div className="mb-2 flex items-start justify-between gap-2">
        <h3 className="text-sm font-medium text-gray-100 leading-snug">
          {task.title}
        </h3>
        <PriorityBadge priority={task.priority} />
      </div>

      {task.labels.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-1">
          {task.labels.map((label) => (
            <span
              key={label}
              className="rounded bg-gray-700 px-1.5 py-0.5 text-[10px] text-gray-300"
            >
              {label}
            </span>
          ))}
        </div>
      )}

      <div className="flex items-center justify-between text-xs text-gray-400">
        <span>{agentName || "Unassigned"}</span>
        <span>{timeAgo(task.created_at)}</span>
      </div>
    </div>
  );
}
