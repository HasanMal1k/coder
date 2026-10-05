"use client";

import { ChevronRightIcon, CodeXmlIcon, MonitorIcon, PlayIcon, SquareIcon } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";
import type { Agent, Architecture, DesignNode, Instance } from "@/lib/api";
import { useAgents } from "@/lib/agents";
import { formatMemory, formatSpecs, formatStack } from "@/lib/format";
import { displayStatus, isBusy, useInstances } from "@/lib/instances";
import { cn } from "@/lib/utils";
import { AgentList } from "../agents/agents-section";
import { StatusBadge, StatusDot } from "../status";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Skeleton } from "../ui/skeleton";
import { Spinner } from "../ui/spinner";
import { Blank, workingOn } from "./shared";

/** A component of the design, and whether its port answers. */
type Running = { node: DesignNode; listening: boolean };

/**
 * How things stand right now: the machines this chat's design runs on, what
 * is live on each, and what every agent it started is doing. Without a chat,
 * it is the same for everything the user has.
 */
export function DashboardTab({
  chatId,
  architecture,
}: {
  chatId?: string;
  architecture: Architecture | null | undefined;
}) {
  const { instances } = useInstances();
  const { agents } = useAgents();

  // What runs on each machine, by the machine's name.
  const components = useMemo(() => {
    const byMachine = new Map<string, Running[]>();
    for (const node of architecture?.design.nodes ?? []) {
      if (!node.machine) continue;
      const state = architecture?.nodes.find((n) => n.id === node.id);
      const on = byMachine.get(node.machine) ?? [];
      on.push({ node, listening: state?.listening === true });
      byMachine.set(node.machine, on);
    }
    return byMachine;
  }, [architecture]);

  const ours = useMemo(
    () => (agents ?? []).filter((agent) => !chatId || agent.chat_id === chatId),
    [agents, chatId],
  );

  // This chat's machines: those its design names, and those its agents have
  // worked on. Without a chat, every machine.
  const [mine, others] = useMemo(() => {
    const all = instances ?? [];
    if (!chatId) return [all, []];

    const ids = new Set(ours.map((agent) => agent.machine.id));
    const mine = all.filter((i) => components.has(i.name) || ids.has(i.id));
    return [mine, all.filter((i) => !mine.includes(i))];
  }, [instances, chatId, components, ours]);

  if (instances === null) {
    return (
      <div className="flex flex-col gap-3 p-4" aria-busy="true" aria-label="Loading">
        <Skeleton className="h-20" />
        <Skeleton className="h-28" />
        <Skeleton className="h-28" />
      </div>
    );
  }

  if (instances.length === 0) {
    return (
      <Blank icon={MonitorIcon} title="No machines yet">
        The assistant creates them when it builds a design. They appear here with what is running on
        each and what every agent is doing.
      </Blank>
    );
  }

  const running = mine.filter((i) => i.status === "running");
  const working = ours.filter((agent) => agent.status === "working").length;

  return (
    <div className="@container/dash min-h-0 flex-1 overflow-y-auto">
      <div className="flex flex-col gap-6 p-4">
        <dl className="grid grid-cols-2 gap-2 @lg/dash:grid-cols-4">
          <Tile label="Machines running" value={`${running.length}`} detail={`of ${mine.length}`} />
          <Tile
            label="Agents working"
            value={`${working}`}
            detail={`of ${ours.length}`}
            live={working > 0}
          />
          <Tile
            label="vCPUs in use"
            value={`${running.reduce((sum, i) => sum + i.cpus, 0)}`}
            detail={`of ${mine.reduce((sum, i) => sum + i.cpus, 0)}`}
          />
          <Tile
            label="Memory in use"
            value={formatMemory(running.reduce((sum, i) => sum + i.memory_mb, 0))}
            detail={`of ${formatMemory(mine.reduce((sum, i) => sum + i.memory_mb, 0))}`}
          />
        </dl>

        <section aria-labelledby="dash-machines" className="flex flex-col gap-2.5">
          <h3 id="dash-machines" className="text-sm font-medium">
            {chatId ? "Machines in this chat" : "Your machines"}
          </h3>
          {mine.length === 0 ? (
            <p className="rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">
              This chat hasn&apos;t created or used a machine yet.
            </p>
          ) : (
            <ul className="flex flex-col gap-2">
              {mine.map((instance) => (
                <MachineCard
                  key={instance.id}
                  instance={instance}
                  components={components.get(instance.name) ?? []}
                  agents={agents}
                />
              ))}
            </ul>
          )}
        </section>

        <section aria-labelledby="dash-agents" className="flex flex-col gap-2.5">
          <h3 id="dash-agents" className="flex items-center gap-2 text-sm font-medium">
            {chatId ? "Agents in this chat" : "Agents"}
            {working > 0 && (
              <Badge variant="secondary" className="gap-1.5">
                <span className="size-1.5 animate-pulse rounded-full bg-success" />
                {working} working
              </Badge>
            )}
          </h3>
          {ours.length === 0 ? (
            <p className="rounded-md border border-dashed px-3 py-4 text-sm text-muted-foreground">
              No agent has been started{chatId ? " in this chat" : ""} yet. The assistant starts one
              for each part it builds.
            </p>
          ) : (
            <div className="overflow-hidden rounded-md border bg-card">
              <AgentList agents={ours} />
            </div>
          )}
        </section>

        {others.length > 0 && (
          <details className="group">
            <summary className="flex cursor-pointer list-none items-center gap-1.5 text-sm font-medium outline-none select-none focus-visible:text-primary [&::-webkit-details-marker]:hidden">
              <ChevronRightIcon className="size-4 transition-transform group-open:rotate-90" />
              Your other machines
              <span className="font-normal text-muted-foreground">{others.length}</span>
            </summary>
            <ul className="mt-2.5 flex flex-col gap-2">
              {others.map((instance) => (
                <MachineCard
                  key={instance.id}
                  instance={instance}
                  components={[]}
                  agents={agents}
                />
              ))}
            </ul>
          </details>
        )}

        <p className="text-xs text-muted-foreground">
          <Link
            href="/machines"
            className="underline underline-offset-4 outline-none hover:text-foreground focus-visible:text-foreground"
          >
            All machines and agents
          </Link>
          , including creating and deleting machines by hand.
        </p>
      </div>
    </div>
  );
}

function Tile({
  label,
  value,
  detail,
  live,
}: {
  label: string;
  value: string;
  detail: string;
  /** Something is happening right now. */
  live?: boolean;
}) {
  return (
    <div className="rounded-md border bg-card px-3 py-2.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 flex items-baseline gap-1.5">
        <span
          className={cn(
            "text-xl font-semibold tracking-tight tabular-nums",
            live && "text-success",
          )}
        >
          {value}
        </span>
        <span className="text-xs text-muted-foreground">{detail}</span>
      </dd>
    </div>
  );
}

function MachineCard({
  instance,
  components,
  agents,
}: {
  instance: Instance;
  components: Running[];
  agents: Agent[] | null;
}) {
  const { pending, start, stop } = useInstances();
  const status = displayStatus(instance, pending[instance.id]);
  const busy = isBusy(status);
  const working = workingOn(agents, instance);

  return (
    <li
      aria-label={instance.name}
      className={cn("rounded-md border bg-card", working && "border-foreground/40")}
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2.5">
        <div className="min-w-0 flex-1">
          <p className="flex flex-wrap items-center gap-2">
            <Link
              href={`/instances/${instance.id}`}
              className="truncate font-mono text-sm font-semibold underline-offset-4 outline-none hover:underline focus-visible:underline"
            >
              {instance.name}
            </Link>
            <StatusBadge status={status} />
            {formatStack(instance) && (
              <Badge variant="outline" className="text-muted-foreground">
                {formatStack(instance)}
              </Badge>
            )}
          </p>
          <p className="mt-0.5 text-xs text-muted-foreground">{formatSpecs(instance)}</p>
        </div>

        <div className="flex shrink-0 items-center gap-1.5">
          {status === "running" ? (
            <>
              <Button asChild size="sm" variant="outline">
                <Link href={`/instances/${instance.id}`}>
                  <CodeXmlIcon data-icon="inline-start" />
                  Open IDE
                </Link>
              </Button>
              <Button
                size="sm"
                variant="ghost"
                aria-label={`Stop ${instance.name}`}
                onClick={() => stop(instance.id)}
              >
                <SquareIcon data-icon="inline-start" />
                Stop
              </Button>
            </>
          ) : (
            <Button
              size="sm"
              variant="outline"
              disabled={busy || status === "creating"}
              aria-label={`Start ${instance.name}`}
              onClick={() => start(instance.id)}
            >
              {busy ? <Spinner data-icon="inline-start" /> : <PlayIcon data-icon="inline-start" />}
              Start
            </Button>
          )}
        </div>
      </div>

      {(components.length > 0 || working) && (
        <ul className="flex flex-col gap-1.5 border-t px-3 py-2.5 text-xs">
          {components.map(({ node, listening }) => (
            <li key={node.id} className="flex items-baseline gap-2">
              <span className="min-w-0 truncate font-medium">{node.name}</span>
              <span className="min-w-0 truncate text-muted-foreground">{node.tech}</span>
              {node.port ? (
                <span
                  className={cn(
                    "ml-auto flex shrink-0 items-center gap-1.5",
                    listening && status === "running" ? "text-success" : "text-muted-foreground",
                  )}
                >
                  <StatusDot status={listening && status === "running" ? "running" : "stopped"} />
                  {listening && status === "running"
                    ? `Live on :${node.port}`
                    : `:${node.port} not up`}
                </span>
              ) : null}
            </li>
          ))}
          {working && (
            <li className="flex items-center gap-2">
              <Spinner className="size-3.5 shrink-0" />
              <span className="min-w-0 truncate" title={working.activity}>
                <span className="font-medium">{working.name}</span>
                <span className="text-muted-foreground">
                  {working.activity ? `: ${working.activity}` : " is working"}
                </span>
              </span>
            </li>
          )}
        </ul>
      )}
    </li>
  );
}
