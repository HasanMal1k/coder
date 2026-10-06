"use client";

import { PlayIcon, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { Agent, Architecture, DesignNode, Instance, NodeKind } from "@/lib/api";
import { useAgents } from "@/lib/agents";
import { displayStatus, isBusy, useInstances } from "@/lib/instances";
import { cn } from "@/lib/utils";
import { Button } from "../ui/button";
import { Spinner } from "../ui/spinner";

/** A component of the design that listens on a port, and how it stands. */
export type Part = {
  node: DesignNode & { port: number; machine: string };
  /** The machine it runs on, once that exists. */
  machine?: Instance;
  /** Something answers on its port. */
  listening: boolean;
};

/**
 * The design's components of the given kinds that listen on a port, each
 * with its machine and whether it is answering. Empty until there is an
 * approved design: a proposal has nothing running to look at.
 */
export function partsOf(
  architecture: Architecture | null | undefined,
  instances: Instance[] | null,
  kinds: NodeKind[],
): Part[] {
  if (!architecture || architecture.status !== "approved") return [];

  return architecture.design.nodes.flatMap((node) => {
    if (!kinds.includes(node.kind) || !node.port || !node.machine) return [];
    const state = architecture.nodes.find((n) => n.id === node.id);

    return [
      {
        node: node as Part["node"],
        machine: instances?.find((i) => i.name === node.machine),
        listening: state?.listening === true,
      },
    ];
  });
}

/** What a tab says when it has nothing to show, in the middle of it. */
export function Blank({
  icon: Icon,
  title,
  children,
  action,
}: {
  icon: LucideIcon;
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-4 overflow-y-auto p-6 text-center">
      <div className="flex size-10 items-center justify-center rounded-md border bg-card text-muted-foreground">
        <Icon className="size-5" />
      </div>
      <div>
        <p className="font-medium">{title}</p>
        {children && (
          <div className="mx-auto mt-2 max-w-sm text-sm text-balance text-muted-foreground">
            {children}
          </div>
        )}
      </div>
      {action}
    </div>
  );
}

/** Chooses among several components; nothing to choose with only one. */
export function PartPicker({
  parts,
  value,
  onChange,
}: {
  parts: Part[];
  value: string;
  onChange: (id: string) => void;
}) {
  if (parts.length < 2) return null;

  return (
    <div
      role="radiogroup"
      aria-label="Component"
      className="flex shrink-0 gap-1 border-b px-3 py-2"
    >
      {parts.map((part) => (
        <button
          key={part.node.id}
          type="button"
          role="radio"
          aria-checked={part.node.id === value}
          onClick={() => onChange(part.node.id)}
          className={cn(
            "flex items-center gap-2 rounded-md border px-2.5 py-1 text-xs transition-colors outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
            part.node.id === value
              ? "border-foreground/60 bg-accent text-foreground"
              : "text-muted-foreground hover:border-foreground/30 hover:text-foreground",
          )}
        >
          <span
            aria-hidden
            className={cn(
              "size-1.5 rounded-full",
              part.listening ? "bg-success" : "bg-muted-foreground/50",
            )}
          />
          {part.node.name}
        </button>
      ))}
    </div>
  );
}

/**
 * What stands between a component and being looked at: no machine yet, a
 * machine that is stopped, or nothing answering on its port. Null when it is
 * up.
 */
export function NotReady({ part, icon }: { part: Part; icon: LucideIcon }) {
  const { pending, start } = useInstances();
  const { agents } = useAgents();
  const { node, machine } = part;

  if (!machine) {
    return (
      <Blank icon={icon} title={`${node.name} isn't built yet`}>
        Its machine, <span className="font-mono text-foreground">{node.machine}</span>, hasn&apos;t
        been created. It is made when the design is built.
      </Blank>
    );
  }

  const status = displayStatus(machine, pending[machine.id]);
  if (status !== "running") {
    const busy = isBusy(status);

    return (
      <Blank
        icon={icon}
        title={busy ? `${machine.name} is ${status}` : `${machine.name} is stopped`}
        action={
          <Button size="sm" disabled={busy} onClick={() => start(machine.id)}>
            {busy ? <Spinner data-icon="inline-start" /> : <PlayIcon data-icon="inline-start" />}
            Start {machine.name}
          </Button>
        }
      >
        {node.name} runs on it. Start the machine and what was running on it comes back by itself.
      </Blank>
    );
  }

  if (!part.listening) {
    const working = workingOn(agents, machine);

    return (
      <Blank icon={icon} title={`Nothing is answering on port ${node.port} yet`}>
        {working ? (
          <span className="inline-flex items-start justify-center gap-2 text-left">
            <Spinner className="mt-0.5 size-3.5 shrink-0" />
            <span className="min-w-0">
              <span className="font-medium text-foreground">{working.name}</span> is working on{" "}
              <span className="font-mono text-foreground">{machine.name}</span>
              {working.activity ? `: ${working.activity}` : ""}
            </span>
          </span>
        ) : (
          <>
            {node.name} should be listening on{" "}
            <span className="font-mono text-foreground">
              {machine.name}:{node.port}
            </span>
            . This appears by itself once it is; if it stays like this, ask the assistant to look.
          </>
        )}
      </Blank>
    );
  }

  return null;
}

/** An agent at work on a machine right now, if there is one. */
export function workingOn(agents: Agent[] | null, machine: Instance) {
  return agents?.find((a) => a.machine.id === machine.id && a.status === "working");
}
