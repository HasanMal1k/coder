"use client";

import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, type ReactNode } from "react";
import type { User } from "@/lib/api";
import { rememberReturnTo, useSession } from "@/lib/session";
import { Spinner } from "../ui/spinner";

const UserContext = createContext<User | null>(null);

/**
 * Renders its children only for a signed-in user. Anyone else is sent to the
 * login page, including a user whose session ends while the page is open, and
 * brought back to the same page once they sign in.
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const session = useSession();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (session === null) {
      rememberReturnTo(pathname);
      router.replace("/login");
    }
  }, [session, router, pathname]);

  if (!session) {
    return (
      <div className="flex h-svh items-center justify-center" role="status" aria-label="Loading">
        <Spinner className="size-5 text-muted-foreground" />
      </div>
    );
  }

  return <UserContext.Provider value={session.user}>{children}</UserContext.Provider>;
}

/** The signed-in user. Only for components rendered inside RequireAuth. */
export function useUser() {
  const user = useContext(UserContext);
  if (!user) throw new Error("useUser must be used inside RequireAuth");
  return user;
}
