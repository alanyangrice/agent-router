"use client";

const PRIORITY_STYLES: Record<string, string> = {
  critical: "bg-red-600/20 text-red-400 ring-1 ring-red-500/30",
  high: "bg-orange-600/20 text-orange-400 ring-1 ring-orange-500/30",
  medium: "bg-blue-600/20 text-blue-400 ring-1 ring-blue-500/30",
  low: "bg-gray-600/20 text-gray-400 ring-1 ring-gray-500/30",
};

const PRIORITY_ICONS: Record<string, string> = {
  critical: "!!!",
  high: "!!",
  medium: "!",
  low: "—",
};

export function PriorityBadge({ priority }: { priority: string }) {
  const style = PRIORITY_STYLES[priority] || PRIORITY_STYLES.medium;
  const icon = PRIORITY_ICONS[priority] || "";

  return (
    <span
      className={`inline-flex items-center gap-1 rounded px-2 py-0.5 text-xs font-semibold ${style}`}
    >
      <span>{icon}</span>
      <span className="capitalize">{priority}</span>
    </span>
  );
}
