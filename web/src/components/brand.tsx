import { SquareTerminalIcon } from "lucide-react";
import { cn } from "@/lib/utils";

/** The product's mark, alone. */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        "grid size-8 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground shadow-sm",
        className,
      )}
    >
      <SquareTerminalIcon className="size-[55%]" />
    </span>
  );
}

/** The product's mark and name. */
export function Brand({ size = "sm" }: { size?: "sm" | "lg" }) {
  const large = size === "lg";

  return (
    <span
      className={cn(
        "inline-flex items-center font-semibold tracking-tight",
        large ? "gap-3 text-2xl" : "gap-2 text-base",
      )}
    >
      <BrandMark className={large ? "size-11 rounded-lg" : undefined} />
      coder
    </span>
  );
}
