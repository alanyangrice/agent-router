"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, Thread } from "@/lib/api";
import { DashboardShell } from "@/components/DashboardShell";

export default function ThreadsPage() {
  const [threads, setThreads] = useState<Thread[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .listThreads()
      .then(setThreads)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <DashboardShell>
      <div className="mb-6">
        <h1 className="text-xl font-semibold text-white">Threads</h1>
        <p className="mt-1 text-sm text-gray-400">
          Conversations between agents and humans.
        </p>
      </div>

      {loading ? (
        <div className="flex h-96 items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" />
        </div>
      ) : threads.length === 0 ? (
        <div className="flex h-64 items-center justify-center rounded-xl border border-gray-800 bg-gray-800/30">
          <p className="text-sm text-gray-500">No threads yet</p>
        </div>
      ) : (
        <div className="space-y-2">
          {threads.map((thread) => (
            <Link
              key={thread.id}
              href={`/threads/${thread.id}`}
              className="flex items-center justify-between rounded-lg border border-gray-800 bg-gray-800/50 px-4 py-3 transition-colors hover:border-gray-700 hover:bg-gray-800"
            >
              <div>
                <h3 className="text-sm font-medium text-gray-100">
                  {thread.name}
                </h3>
                <p className="mt-0.5 text-xs text-gray-500">
                  {thread.type}
                  {thread.task_id && ` · Task ${thread.task_id.slice(0, 8)}`}
                </p>
              </div>
              <span className="text-xs text-gray-500">
                {new Date(thread.created_at).toLocaleDateString()}
              </span>
            </Link>
          ))}
        </div>
      )}
    </DashboardShell>
  );
}
