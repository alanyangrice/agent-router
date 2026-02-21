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
  const activeStatuses = ["in_progress", "in_qa", "in_review"];
  if (!activeStatuses.includes(task.status) || !task.started_at) return false;
  const elapsed = Date.now() - new Date(task.started_at).getTime();
  return elapsed > 30 * 60 * 1000; // 30 minutes
}

const ROLE_COLORS: Record<string, string> = {
  coder:    "text-blue-400",
  qa:       "text-orange-400",
  reviewer: "text-yellow-400",
};

export function TaskCard({
  task,
  agentName,
  agentRole,
  onClick,
}: {
  task: Task;
  agentName?: string;
  agentRole?: string;
  onClick?: () => void;
}) {
  const stuck = isStuck(task);
  const roleColor = agentRole ? (ROLE_COLORS[agentRole] ?? "text-gray-400") : "text-gray-500";

  return (
    <div
      onClick={onClick}
      className={`group relative rounded-lg border p-3 transition-colors hover:border-gray-600 ${
        onClick ? "cursor-pointer" : ""
      } ${
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

      {task.pr_url && (
        <a
          href={task.pr_url}
          target="_blank"
          rel="noopener noreferrer"
          onClick={(e) => e.stopPropagation()}
          className="mb-2 flex items-center gap-1 text-[11px] text-blue-400 hover:text-blue-300 hover:underline"
        >
          <svg className="h-3 w-3 shrink-0" fill="currentColor" viewBox="0 0 20 20">
            <path d="M11 3a1 1 0 100 2h2.586l-6.293 6.293a1 1 0 101.414 1.414L15 6.414V9a1 1 0 102 0V4a1 1 0 00-1-1h-5z" />
            <path d="M5 5a2 2 0 00-2 2v8a2 2 0 002 2h8a2 2 0 002-2v-3a1 1 0 10-2 0v3H5V7h3a1 1 0 000-2H5z" />
          </svg>
          <span className="truncate max-w-[120px]">
            {task.pr_url.replace("https://github.com/", "")}
          </span>
        </a>
      )}

      {task.labels && task.labels.length > 0 && (
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

      <div className="flex items-center justify-between text-xs">
        <span className={agentName ? roleColor : "text-gray-500"}>
          {agentName
            ? `${agentRole ? `[${agentRole}] ` : ""}${agentName}`
            : "Unassigned"}
        </span>
        <span className="text-gray-500">{timeAgo(task.created_at)}</span>
      </div>
    </div>
  );
}
