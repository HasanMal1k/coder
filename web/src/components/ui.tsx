import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from "react";

type ButtonVariant = "primary" | "secondary" | "danger" | "ghost";

const BUTTON_VARIANTS: Record<ButtonVariant, string> = {
  primary:
    "bg-accent text-on-accent hover:bg-accent-strong disabled:bg-accent/40 disabled:text-on-accent/70",
  secondary:
    "border border-line-strong bg-raised text-fg hover:border-muted disabled:text-muted disabled:hover:border-line-strong",
  danger:
    "border border-danger/50 text-danger hover:bg-danger/10 disabled:opacity-50",
  ghost: "text-muted hover:text-fg disabled:opacity-50",
};

export function buttonClass(variant: ButtonVariant = "secondary", extra = "") {
  return [
    "inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-md px-3.5 text-sm font-medium whitespace-nowrap",
    "transition-colors outline-none focus-visible:ring-2 focus-visible:ring-accent/70",
    "disabled:cursor-not-allowed",
    BUTTON_VARIANTS[variant],
    extra,
  ].join(" ");
}

export function Button({
  variant = "secondary",
  className = "",
  type = "button",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant }) {
  return <button type={type} className={buttonClass(variant, className)} {...props} />;
}

export function Field({
  label,
  hint,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: string }) {
  return (
    <label className="flex flex-col gap-1.5 text-sm">
      <span className="text-muted">{label}</span>
      <input
        className="h-10 rounded-md border border-line-strong bg-bg px-3 text-fg outline-none placeholder:text-muted/60 focus:border-accent focus:ring-1 focus:ring-accent"
        {...props}
      />
      {hint && <span className="text-xs text-muted">{hint}</span>}
    </label>
  );
}

export function ErrorNote({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
      {children}
    </p>
  );
}

export function Spinner({ className = "" }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={`inline-block size-4 animate-spin rounded-full border-2 border-current border-t-transparent ${className}`}
    />
  );
}

/** Fills the page while something that decides what to show is loading. */
export function PageSpinner({ label = "Loading" }: { label?: string }) {
  return (
    <div className="flex flex-1 items-center justify-center gap-3 text-sm text-muted" role="status">
      <Spinner />
      {label}
    </div>
  );
}
