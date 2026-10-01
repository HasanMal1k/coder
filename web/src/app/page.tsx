"use client";

import { Dashboard } from "@/components/dashboard";
import { RequireAuth } from "@/components/require-auth";

export default function HomePage() {
  return <RequireAuth>{(session) => <Dashboard session={session} />}</RequireAuth>;
}
