"use client";

import { useEffect, useState } from "react";
import { api, Task, Agent, Thread } from "@/lib/api";
import { PriorityBadge } from "./PriorityBadge";
import { StatusBadge } from "./StatusBadge";

function formatDate(dateStr?: string): string {
  if (!dateStr) return "—";
  return new Date(dateStr).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function TaskDetailPanel({
  task,
  onClose,
}: {
  task: Task;
  onClose: () => void;
}) {
  const [agent, setAgent] = useState<Agent | null>(null);
  const [thread, setThread] = useState<Thread | null>(null);

  useEffect(() => {
    if (task.assigned_agent_id) {
      api.getAgent(task.assigned_agent_id).then(setAgent).catch(() => null);
    } else {
      setAgent(null);
    }

    api
      .listThreads({ task_id: task.id })
      .then((threads) => setThread(threads[0] ?? null))
      .catch(() => null);
  }, [task.id, task.assigned_agent_id]);

  // Close on Escape key
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onClose]);

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-black/40"
        onClick={onClose}
      />

      {/* Panel */}
      <div className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col border-l border-gray-800 bg-gray-950 shadow-2xl">
        {/* Header */}
        <div className="flex items-start justify-between border-b border-gray-800 px-5 py-4">
          <div className="flex-1 pr-4">
            <h2 className="text-base font-semibold text-white leading-snug">
              {task.title}
            </h2>
            <button
              className="mt-0.5 text-xs text-gray-500 font-mono hover:text-gray-300 transition-colors cursor-copy"
              title="Click to copy"
              onClick={() => navigator.clipboard.writeText(task.id)}
            >
              {task.id}
            </button>
          </div>
          <button
            onClick={onClose}
            className="shrink-0 rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-800 hover:text-gray-200"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">
          {/* Status + Priority */}
          <div className="flex items-center gap-3">
            <StatusBadge status={task.status} />
            <PriorityBadge priority={task.priority} />
          </div>

          {/* Description */}
          {task.description ? (
            <div>
              <p className="mb-1.5 text-xs font-medium uppercase tracking-wider text-gray-500">
                Description
              </p>
              <p className="text-sm text-gray-300 whitespace-pre-wrap leading-relaxed">
                {task.description}
              </p>
            </div>
          ) : (
            <p className="text-sm text-gray-600 italic">No description provided.</p>
          )}

          {/* Assigned Agent */}
          <div>
            <p className="mb-1.5 text-xs font-medium uppercase tracking-wider text-gray-500">
              Assigned Agent
            </p>
            {agent ? (
              <div className="flex items-center gap-2.5 rounded-lg bg-gray-900 px-3 py-2.5">
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-gray-700 text-sm font-semibold text-gray-200">
                  {agent.name.charAt(0).toUpperCase()}
                </div>
                <div>
                  <p className="text-sm font-medium text-gray-200">{agent.name}</p>
                  <p className="text-xs text-gray-500">{agent.role} · {agent.model}</p>
                </div>
                <StatusBadge status={agent.status} className="ml-auto" />
              </div>
            ) : (
              <p className="text-sm text-gray-500">Unassigned</p>
            )}
          </div>

          {/* Branch */}
          <div>
            <p className="mb-1.5 text-xs font-medium uppercase tracking-wider text-gray-500">
              Branch
            </p>
            <code className="rounded bg-gray-900 px-2.5 py-1.5 text-xs text-gray-300">
              {task.branch_name || "—"}
            </code>
            <span className="ml-2 text-xs text-gray-500">({task.branch_type})</span>
          </div>

          {/* Labels */}
          {task.labels && task.labels.length > 0 && (
            <div>
              <p className="mb-1.5 text-xs font-medium uppercase tracking-wider text-gray-500">
                Labels
              </p>
              <div className="flex flex-wrap gap-1.5">
                {task.labels.map((label) => (
                  <span
                    key={label}
                    className="rounded bg-gray-800 px-2 py-0.5 text-xs text-gray-300"
                  >
                    {label}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Thread */}
          <div>
            <p className="mb-1.5 text-xs font-medium uppercase tracking-wider text-gray-500">
              Thread
            </p>
            {thread ? (
              <a
                href={`/threads/${thread.id}`}
                className="flex items-center gap-2 rounded-lg bg-gray-900 px-3 py-2.5 text-sm text-blue-400 transition-colors hover:text-blue-300"
              >
                <svg className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
                </svg>
                {thread.name}
              </a>
            ) : (
              <p className="text-sm text-gray-500">No thread yet.</p>
            )}
          </div>

          {/* Timestamps */}
          <div className="border-t border-gray-800 pt-4 space-y-2">
            <Row label="Created by" value={task.created_by} />
            <Row label="Created" value={formatDate(task.created_at)} />
            <Row label="Updated" value={formatDate(task.updated_at)} />
            {task.started_at && (
              <Row label="Started" value={formatDate(task.started_at)} />
            )}
            {task.completed_at && (
              <Row label="Completed" value={formatDate(task.completed_at)} />
            )}
          </div>
        </div>
      </div>
    </>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between text-xs">
      <span className="text-gray-500">{label}</span>
      <span className="text-gray-300">{value}</span>
    </div>
  );
}
