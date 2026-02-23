"use client";

const STATUS_STYLES: Record<string, string> = {
  backlog:     "bg-gray-600 text-gray-200",
  ready:       "bg-purple-600 text-purple-100",
  in_progress: "bg-blue-600 text-blue-100",
  in_qa:       "bg-orange-600 text-orange-100",
  in_review:   "bg-yellow-600 text-yellow-100",
  merged:      "bg-green-600 text-green-100",
  // agent statuses
  idle:        "bg-green-600 text-green-100",
  working:     "bg-blue-600 text-blue-100",
  blocked:     "bg-yellow-600 text-yellow-100",
  offline:     "bg-red-600 text-red-100",
};

const STATUS_LABELS: Record<string, string> = {
  backlog:     "Backlog",
  ready:       "Ready",
  in_progress: "In Progress",
  in_qa:       "QA",
  in_review:   "Review",
  merged:      "Merged",
  idle:        "Idle",
  working:     "Working",
  blocked:     "Blocked",
  offline:     "Offline",
};

export function StatusBadge({ status, className = "" }: { status: string; className?: string }) {
  const style = STATUS_STYLES[status] || "bg-gray-600 text-gray-200";
  const label = STATUS_LABELS[status] || status;

  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${style} ${className}`}
    >
      {label}
    </span>
  );
}
