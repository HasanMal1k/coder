"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { clearSession, type Session } from "@/lib/session";
import { Button } from "./ui";

export function AppHeader({ session, children }: { session: Session; children?: ReactNode }) {
  return (
    <header className="flex h-14 shrink-0 items-center gap-4 border-b border-line bg-surface px-4 sm:px-6">
      <Link href="/" className="font-mono text-sm font-semibold tracking-tight text-fg">
        <span className="text-accent">▍</span>coder
      </Link>

      <div className="flex min-w-0 flex-1 items-center gap-3">{children}</div>

      <span className="hidden truncate text-sm text-muted sm:block" title={session.user.email}>
        {session.user.display_name || session.user.email}
      </span>
      <Button variant="ghost" className="px-2" onClick={() => clearSession()}>
        Sign out
      </Button>
    </header>
  );
}
