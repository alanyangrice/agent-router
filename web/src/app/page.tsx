"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";

export default function SetupPage() {
  const router = useRouter();

  useEffect(() => {
    const init = async () => {
      const existing = localStorage.getItem("agent_mesh_project_id");
      if (existing) {
        router.replace("/board");
        return;
      }

      try {
        const project = await api.createProject({
          name: "Default",
          repo_url: "https://github.com/placeholder/repo",
        });
        localStorage.setItem("agent_mesh_project_id", project.id);
      } catch (err) {
        console.error("Failed to auto-create project:", err);
      }

      router.replace("/board");
    };

    init();
  }, [router]);

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" />
    </div>
  );
}
