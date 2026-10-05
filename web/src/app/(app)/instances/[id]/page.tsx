"use client";

import { useParams } from "next/navigation";
import { Workspace } from "@/components/workspace/workspace";

export default function InstancePage() {
  const { id } = useParams<{ id: string }>();

  // Keyed by machine, so moving between machines starts each with a clean
  // slate instead of carrying one's terminal into the next.
  return <Workspace key={id} id={id} />;
}
