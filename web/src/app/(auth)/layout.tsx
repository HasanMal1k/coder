import type { ReactNode } from "react";
import { Brand } from "@/components/brand";

export default function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <main className="relative flex flex-1 flex-col items-center justify-center gap-8 overflow-hidden px-4 py-10">
      {/* A faint grid behind the form, fading out towards the edges. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 bg-[linear-gradient(to_right,var(--border)_1px,transparent_1px),linear-gradient(to_bottom,var(--border)_1px,transparent_1px)] mask-[radial-gradient(60%_50%_at_50%_45%,black,transparent)] bg-size-[48px_48px] opacity-60"
      />
      <Brand size="lg" />
      {children}
    </main>
  );
}
