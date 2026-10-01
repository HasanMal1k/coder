"use client";

import { useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";
import { useSession, type Session } from "@/lib/session";
import { PageSpinner } from "./ui";

/**
 * Renders its children only for a signed-in user, handing them the session.
 * Anyone else is sent to the login page, including a user whose session ends
 * while the page is open.
 */
export function RequireAuth({ children }: { children: (session: Session) => ReactNode }) {
  const session = useSession();
  const router = useRouter();

  useEffect(() => {
    if (session === null) router.replace("/login");
  }, [session, router]);

  if (!session) return <PageSpinner />;

  return children(session);
}
