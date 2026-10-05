"use client";

import { CheckIcon, ChevronRightIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  approveArchitecture,
  errorMessage,
  type Architecture,
  type Design,
  type PowerName,
} from "@/lib/api";
import { useAgents } from "@/lib/agents";
import { useInstances } from "@/lib/instances";
import { ArchitectureCanvas } from "../architecture/canvas";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Spinner } from "../ui/spinner";

const STRUCTURES: Record<Design["structure"], string> = {
  "single-app": "One application",
  "frontend-and-backend": "Frontend and backend",
  microservices: "Microservices",
};

const REPOSITORIES: Record<Design["repository"], string> = {
  "one-repository": "One repository",
  "repository-per-component": "A repository per component",
};

/**
 * The design of what a chat is building, as a diagram. While it is a
 * proposal the user can change it and agree to it here; once it is being
 * built, the diagram shows how each part stands.
 */
export function GraphTab({
  chatId,
  architecture,
  onChanged,
}: {
  chatId?: string;
  /** undefined while loading; null if the chat has no design. */
  architecture: Architecture | null | undefined;
  onChanged: () => void;
}) {
  if (chatId && architecture === undefined) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Spinner className="text-muted-foreground" aria-label="Loading the design" />
      </div>
    );
  }
  if (!chatId || !architecture) return <NoDesign />;

  // Keyed by version, so changes the user made to one proposal aren't
  // carried over onto the next.
  return (
    <DesignView
      key={architecture.updated_at}
      chatId={chatId}
      architecture={architecture}
      onChanged={onChanged}
    />
  );
}

/** What the tab says before there is a design to draw. */
function NoDesign() {
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-6 overflow-y-auto p-6 text-center">
      {/* A hint of what will be here: three parts, joined up. */}
      <div aria-hidden className="flex items-center text-muted-foreground/50">
        <div className="h-12 w-20 rounded-md border border-dashed border-current" />
        <div className="w-8 border-t border-dashed border-current" />
        <div className="h-12 w-20 rounded-md border border-dashed border-current" />
        <div className="w-8 border-t border-dashed border-current" />
        <div className="h-12 w-20 rounded-md border border-dashed border-current" />
      </div>
      <div>
        <p className="font-medium">The design appears here</p>
        <p className="mx-auto mt-2 max-w-sm text-sm text-balance text-muted-foreground">
          Once the assistant knows what you want, it draws the parts of your project and how they
          connect, for you to approve before anything is built. After that, each part shows its
          machine and whether it is running, and pointing at one opens its editor.
        </p>
      </div>
    </div>
  );
}

function DesignView({
  chatId,
  architecture,
  onChanged,
}: {
  chatId: string;
  architecture: Architecture;
  onChanged: () => void;
}) {
  const { instances } = useInstances();
  const { agents } = useAgents();

  // Sizes the user has changed, by machine, while the design is a proposal.
  const [resized, setResized] = useState<Record<string, { size?: PowerName; disk_gb?: number }>>(
    {},
  );
  const [approving, setApproving] = useState(false);

  const proposed = architecture.status === "proposed";
  const edited = Object.keys(resized).length > 0;

  // The design with those changes applied. Components sharing a machine
  // describe the same machine, so a change to it reaches all of them.
  const design = useMemo<Design>(
    () => ({
      ...architecture.design,
      nodes: architecture.design.nodes.map((node) =>
        node.machine && resized[node.machine] ? { ...node, ...resized[node.machine] } : node,
      ),
    }),
    [architecture.design, resized],
  );

  function resize(machine: string, change: { size?: PowerName; disk_gb?: number }) {
    setResized((all) => ({ ...all, [machine]: { ...all[machine], ...change } }));
  }

  async function approve() {
    setApproving(true);
    try {
      await approveArchitecture(chatId, edited ? design : undefined);
      onChanged();
    } catch (err) {
      toast.error("Couldn't approve the design", { description: errorMessage(err) });
      setApproving(false);
    }
  }

  const machines = new Set(design.nodes.map((n) => n.machine).filter(Boolean)).size;

  return (
    <>
      <div className="flex shrink-0 flex-col gap-2 border-b px-4 py-3">
        <p className="text-sm text-muted-foreground">{design.summary}</p>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {proposed ? (
            <Badge variant="outline" className="border-warning/40 text-warning">
              Proposed
            </Badge>
          ) : (
            <Badge variant="secondary" className="text-success">
              <CheckIcon />
              Approved
            </Badge>
          )}
          <Badge variant="outline">{STRUCTURES[design.structure]}</Badge>
          <Badge variant="outline">{REPOSITORIES[design.repository]}</Badge>
          <span>
            {design.nodes.length} component{design.nodes.length === 1 ? "" : "s"} on {machines}{" "}
            machine{machines === 1 ? "" : "s"}
          </span>
        </div>
      </div>

      {proposed && (
        <div className="flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b bg-muted/40 px-4 py-2.5">
          <p className="min-w-48 flex-1 text-xs text-muted-foreground">
            Nothing is built until you approve. Point at a component to change its machine&apos;s
            size, or say in the chat what to change.
          </p>
          <Button size="sm" onClick={approve} disabled={approving}>
            {approving ? (
              <Spinner data-icon="inline-start" />
            ) : (
              <CheckIcon data-icon="inline-start" />
            )}
            {edited ? "Approve with my changes" : "Approve and build"}
          </Button>
        </div>
      )}

      <ArchitectureCanvas
        className="flex-1"
        architecture={architecture}
        design={design}
        instances={instances ?? []}
        agents={agents ?? []}
        onResize={proposed ? resize : undefined}
      />

      {design.reasons.length > 0 && (
        <details className="group shrink-0 border-t">
          <summary className="flex cursor-pointer list-none items-center gap-1.5 px-4 py-2.5 text-sm font-medium outline-none select-none focus-visible:text-primary [&::-webkit-details-marker]:hidden">
            <ChevronRightIcon className="size-4 transition-transform group-open:rotate-90" />
            Why it is designed this way
          </summary>
          <ul className="max-h-48 list-disc space-y-1.5 overflow-y-auto px-4 pb-4 pl-10 text-sm text-muted-foreground marker:text-muted-foreground/60">
            {design.reasons.map((reason) => (
              <li key={reason}>{reason}</li>
            ))}
            {design.nodes
              .filter((node) => node.why)
              .map((node) => (
                <li key={node.id}>
                  <span className="font-medium text-foreground">{node.name}:</span> {node.why}
                </li>
              ))}
          </ul>
        </details>
      )}
    </>
  );
}
