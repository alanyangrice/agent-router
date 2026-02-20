"use client";

import { useEffect, useState } from "react";
import { DashboardShell } from "@/components/DashboardShell";

interface ProjectConfig {
  id: string;
  name: string;
  repo_url: string;
}

const ENV_VARS = [
  { key: "NEXT_PUBLIC_API_URL", label: "API URL", fallback: "http://localhost:8080" },
  { key: "NEXT_PUBLIC_WS_URL", label: "WebSocket URL", fallback: "ws://localhost:8080/api/ws" },
  { key: "GITHUB_TOKEN", label: "GitHub Token", sensitive: true },
  { key: "OPENAI_API_KEY", label: "OpenAI API Key", sensitive: true },
  { key: "ANTHROPIC_API_KEY", label: "Anthropic API Key", sensitive: true },
];

export default function SettingsPage() {
  const [project, setProject] = useState<ProjectConfig | null>(null);

  useEffect(() => {
    const id = localStorage.getItem("agent_mesh_project_id");
    if (id) {
      setProject({ id, name: "Current Project", repo_url: "" });
    }
  }, []);

  const handleReset = () => {
    localStorage.removeItem("agent_mesh_project_id");
    window.location.href = "/";
  };

  return (
    <DashboardShell>
      <div className="mb-6">
        <h1 className="text-xl font-semibold text-white">Settings</h1>
        <p className="mt-1 text-sm text-gray-400">
          Project configuration and environment status.
        </p>
      </div>

      <div className="max-w-2xl space-y-6">
        <section className="rounded-xl border border-gray-800 bg-gray-800/50 p-5">
          <h2 className="mb-4 text-sm font-semibold text-gray-200">
            Project
          </h2>
          {project ? (
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-400">Project ID</span>
                <code className="rounded bg-gray-900 px-2 py-1 text-xs text-gray-300">
                  {project.id}
                </code>
              </div>
              <div className="border-t border-gray-700/50 pt-3">
                <button
                  onClick={handleReset}
                  className="rounded-lg bg-red-600/10 px-4 py-2 text-sm font-medium text-red-400 transition-colors hover:bg-red-600/20"
                >
                  Reset Project
                </button>
                <p className="mt-1.5 text-xs text-gray-500">
                  Clears the stored project ID and returns to setup.
                </p>
              </div>
            </div>
          ) : (
            <p className="text-sm text-gray-500">
              No project configured.{" "}
              <a href="/" className="text-blue-400 hover:underline">
                Set up a project
              </a>
            </p>
          )}
        </section>

        <section className="rounded-xl border border-gray-800 bg-gray-800/50 p-5">
          <h2 className="mb-4 text-sm font-semibold text-gray-200">
            Environment
          </h2>
          <div className="space-y-2">
            {ENV_VARS.map((v) => {
              const isSet = v.sensitive
                ? undefined
                : typeof window !== "undefined"
                  ? !!process.env[v.key] || !!v.fallback
                  : false;

              return (
                <div
                  key={v.key}
                  className="flex items-center justify-between rounded-lg bg-gray-900/50 px-3 py-2.5"
                >
                  <div>
                    <p className="text-sm text-gray-300">{v.label}</p>
                    <code className="text-xs text-gray-500">{v.key}</code>
                  </div>
                  {v.sensitive ? (
                    <span className="rounded bg-gray-700 px-2 py-0.5 text-xs text-gray-400">
                      Server-side
                    </span>
                  ) : isSet ? (
                    <span className="flex items-center gap-1.5 text-xs text-green-400">
                      <span className="h-1.5 w-1.5 rounded-full bg-green-400" />
                      Configured
                    </span>
                  ) : (
                    <span className="flex items-center gap-1.5 text-xs text-gray-500">
                      <span className="h-1.5 w-1.5 rounded-full bg-gray-500" />
                      Not set
                    </span>
                  )}
                </div>
              );
            })}
          </div>
        </section>

        <section className="rounded-xl border border-gray-800 bg-gray-800/50 p-5">
          <h2 className="mb-4 text-sm font-semibold text-gray-200">About</h2>
          <div className="space-y-2 text-sm text-gray-400">
            <p>Agent Mesh Dashboard v0.1.0</p>
            <p>
              Multi-agent orchestration system for collaborative software development.
            </p>
          </div>
        </section>
      </div>
    </DashboardShell>
  );
}
