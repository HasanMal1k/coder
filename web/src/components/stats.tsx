import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Skeleton } from "./ui/skeleton";

/**
 * A few figures in one box, with a line between each: two to a row where
 * there is little room, and `className` says from what width they share one.
 * Give it an even number of figures, so no cell is left empty.
 */
export function Stats({ className, children }: { className?: string; children: ReactNode }) {
  // The lines are the box's own colour showing through the gaps between cells.
  return (
    <dl
      className={cn(
        "grid grid-cols-2 gap-px overflow-hidden rounded-lg border bg-border",
        className,
      )}
    >
      {children}
    </dl>
  );
}

/** One figure: what it counts, the count, and what it is out of. */
export function Stat({
  label,
  value,
  detail,
  tone,
}: {
  label: string;
  /** null while loading. */
  value: string | null;
  detail?: string;
  /** A colour for the figure, when it says something: green for at work. */
  tone?: string;
}) {
  return (
    <div className="bg-card px-4 py-3">
      <dt className="truncate text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 flex items-baseline gap-1.5">
        {value === null ? (
          <Skeleton className="my-1 h-5 w-10" />
        ) : (
          <>
            <span className={cn("text-xl font-semibold tracking-tight tabular-nums", tone)}>
              {value}
            </span>
            {detail && (
              <span className="truncate text-xs text-muted-foreground tabular-nums">{detail}</span>
            )}
          </>
        )}
      </dd>
    </div>
  );
}
