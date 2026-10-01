import type { InstanceStatus } from "@/lib/api";

const TONES: Record<InstanceStatus, { dot: string; text: string; pulse: boolean }> = {
  running: { dot: "bg-accent", text: "text-accent", pulse: false },
  stopped: { dot: "bg-muted", text: "text-muted", pulse: false },
  error: { dot: "bg-danger", text: "text-danger", pulse: false },
  creating: { dot: "bg-warn", text: "text-warn", pulse: true },
  starting: { dot: "bg-warn", text: "text-warn", pulse: true },
  stopping: { dot: "bg-warn", text: "text-warn", pulse: true },
  deleting: { dot: "bg-warn", text: "text-warn", pulse: true },
};

export function StatusBadge({ status }: { status: InstanceStatus }) {
  const tone = TONES[status];

  return (
    <span className={`inline-flex items-center gap-2 font-mono text-xs ${tone.text}`}>
      <span className={`size-2 rounded-full ${tone.dot} ${tone.pulse ? "animate-pulse" : ""}`} />
      {status}
    </span>
  );
}
