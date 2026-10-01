"use client";

import { useParams } from "next/navigation";
import { RequireAuth } from "@/components/require-auth";
import { Workspace } from "@/components/workspace";

export default function InstancePage() {
  const { id } = useParams<{ id: string }>();

  return <RequireAuth>{(session) => <Workspace key={id} session={session} id={id} />}</RequireAuth>;
}
