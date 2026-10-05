import type { InstanceStatus } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "./ui/badge";
import { Spinner } from "./ui/spinner";

const TONES: Record<InstanceStatus, { dot: string; badge: string; label: string; busy: boolean }> =
  {
    running: {
      dot: "bg-success",
      badge: "border-success/30 bg-success/10 text-success",
      label: "Running",
      busy: false,
    },
    stopped: {
      dot: "bg-muted-foreground/60",
      badge: "text-muted-foreground",
      label: "Stopped",
      busy: false,
    },
    error: {
      dot: "bg-destructive",
      badge: "border-destructive/30 bg-destructive/10 text-destructive",
      label: "Error",
      busy: false,
    },
    creating: {
      dot: "bg-warning",
      badge: "border-warning/30 bg-warning/10 text-warning",
      label: "Creating",
      busy: true,
    },
    starting: {
      dot: "bg-warning",
      badge: "border-warning/30 bg-warning/10 text-warning",
      label: "Starting",
      busy: true,
    },
    stopping: {
      dot: "bg-warning",
      badge: "border-warning/30 bg-warning/10 text-warning",
      label: "Stopping",
      busy: true,
    },
    deleting: {
      dot: "bg-warning",
      badge: "border-warning/30 bg-warning/10 text-warning",
      label: "Deleting",
      busy: true,
    },
  };

export function statusLabel(status: InstanceStatus) {
  return TONES[status].label;
}

/** Just the coloured dot, for places too small for words. */
export function StatusDot({ status, className }: { status: InstanceStatus; className?: string }) {
  const tone = TONES[status];

  return (
    <span
      role="img"
      aria-label={tone.label}
      title={tone.label}
      className={cn(
        "inline-block size-2 shrink-0 rounded-full",
        tone.dot,
        tone.busy && "animate-pulse",
        className,
      )}
    />
  );
}

/** A machine's status in words, with a dot, or a spinner while it is changing. */
export function StatusBadge({ status, className }: { status: InstanceStatus; className?: string }) {
  const tone = TONES[status];

  return (
    <Badge variant="outline" className={cn("gap-1.5 font-medium", tone.badge, className)}>
      {tone.busy ? (
        <Spinner className="size-3" />
      ) : (
        <span className={cn("size-1.5 rounded-full", tone.dot)} />
      )}
      {tone.label}
    </Badge>
  );
}
