"use client";

import { useEffect, useState, useCallback } from "react";
import { api, RolePrompt } from "@/lib/api";
import { DashboardShell } from "@/components/DashboardShell";

const ROLES = [
  {
    key: "coder",
    label: "Coder",
    description: "Implements features, writes code, opens PRs",
    color: "text-blue-400",
    bg: "bg-blue-900/20 border-blue-800/40",
  },
  {
    key: "qa",
    label: "QA",
    description: "Runs tests, reports failures, validates acceptance criteria",
    color: "text-orange-400",
    bg: "bg-orange-900/20 border-orange-800/40",
  },
  {
    key: "reviewer",
    label: "Reviewer",
    description: "Reviews code quality, approves or requests changes, merges PRs",
    color: "text-yellow-400",
    bg: "bg-yellow-900/20 border-yellow-800/40",
  },
];

export default function PromptsPage() {
  const [projectId, setProjectId] = useState<string | null>(null);
  const [prompts, setPrompts] = useState<Record<string, string>>({});
  const [editing, setEditing] = useState<string | null>(null);
  const [editContent, setEditContent] = useState("");
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const id = localStorage.getItem("agent_mesh_project_id");
    setProjectId(id);
  }, []);

  const fetchPrompts = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    try {
      const results = await Promise.allSettled(
        ROLES.map((r) => api.getPrompt(projectId, r.key))
      );
      const map: Record<string, string> = {};
      results.forEach((result, i) => {
        if (result.status === "fulfilled") {
          map[ROLES[i].key] = result.value.content;
        }
      });
      setPrompts(map);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    fetchPrompts();
  }, [fetchPrompts]);

  const handleEdit = (role: string) => {
    setEditing(role);
    setEditContent(prompts[role] ?? "");
  };

  const handleSave = async () => {
    if (!projectId || !editing) return;
    setSaving(true);
    try {
      await api.setPrompt(projectId, editing, editContent);
      setPrompts((prev) => ({ ...prev, [editing]: editContent }));
      setSaved(editing);
      setEditing(null);
      setTimeout(() => setSaved(null), 2000);
    } catch (err) {
      alert("Failed to save prompt: " + (err instanceof Error ? err.message : "Unknown error"));
    } finally {
      setSaving(false);
    }
  };

  if (!projectId) {
    return (
      <DashboardShell>
        <div className="flex h-64 items-center justify-center rounded-xl border border-gray-800 bg-gray-800/30">
          <p className="text-sm text-gray-500">
            No project selected. Complete the setup wizard first.
          </p>
        </div>
      </DashboardShell>
    );
  }

  return (
    <DashboardShell>
      <div className="mb-6">
        <h1 className="text-xl font-semibold text-white">Role Prompts</h1>
        <p className="mt-1 text-sm text-gray-400">
          System prompts delivered to agents via MCP{" "}
          <code className="rounded bg-gray-800 px-1.5 py-0.5 text-xs text-gray-300">
            prompts/get
          </code>
          . Agents fetch their prompt once at session startup. Changes take
          effect on the next agent connection.
        </p>
      </div>

      {loading ? (
        <div className="flex h-64 items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" />
        </div>
      ) : (
        <div className="space-y-6">
          {ROLES.map((role) => {
            const isEditing = editing === role.key;
            const wasSaved = saved === role.key;
            const content = prompts[role.key] ?? "";

            return (
              <div
                key={role.key}
                className={`rounded-xl border p-5 ${role.bg}`}
              >
                <div className="mb-3 flex items-start justify-between">
                  <div>
                    <h2 className={`text-base font-semibold ${role.color}`}>
                      {role.label}
                    </h2>
                    <p className="mt-0.5 text-xs text-gray-400">
                      {role.description}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    {wasSaved && (
                      <span className="text-xs text-green-400">Saved ✓</span>
                    )}
                    {!isEditing && (
                      <button
                        onClick={() => handleEdit(role.key)}
                        className="rounded-lg border border-gray-600 bg-gray-800 px-3 py-1.5 text-xs text-gray-300 hover:border-gray-500 hover:text-white transition-colors"
                      >
                        Edit
                      </button>
                    )}
                  </div>
                </div>

                {isEditing ? (
                  <div className="space-y-3">
                    <textarea
                      value={editContent}
                      onChange={(e) => setEditContent(e.target.value)}
                      rows={12}
                      className="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 placeholder-gray-600 outline-none focus:border-blue-500"
                      placeholder="Enter the system prompt for this role..."
                    />
                    <div className="flex items-center gap-3">
                      <button
                        onClick={handleSave}
                        disabled={saving}
                        className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50 transition-colors"
                      >
                        {saving ? "Saving..." : "Save Prompt"}
                      </button>
                      <button
                        onClick={() => setEditing(null)}
                        className="text-sm text-gray-400 hover:text-gray-200"
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                ) : (
                  <div className="rounded-lg bg-gray-900/60 p-3">
                    {content ? (
                      <pre className="whitespace-pre-wrap font-mono text-xs text-gray-300 max-h-48 overflow-y-auto leading-relaxed">
                        {content}
                      </pre>
                    ) : (
                      <p className="text-xs italic text-gray-500">
                        Using global default prompt (seeded in migration). Click Edit to customize for this project.
                      </p>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      <div className="mt-8 rounded-xl border border-gray-800 bg-gray-800/30 p-5">
        <h3 className="mb-2 text-sm font-medium text-gray-300">
          How MCP Prompts Work
        </h3>
        <div className="space-y-2 text-xs text-gray-400">
          <p>
            1. Agent connects to <code className="text-gray-300">/mcp</code> and calls{" "}
            <code className="text-gray-300">register_agent</code>.
          </p>
          <p>
            2. Agent calls{" "}
            <code className="text-gray-300">prompts/get(name="coder", arguments={"{"}"project_id": "..."{"}"})</code>.
          </p>
          <p>
            3. Server returns the project-specific prompt (or falls back to the global default).
          </p>
          <p>
            4. Agent uses this as its LLM system prompt for the entire session.
          </p>
          <p className="text-gray-500 italic">
            Changes take effect the next time an agent starts a new MCP session — running agents keep their current prompt.
          </p>
        </div>
      </div>
    </DashboardShell>
  );
}
