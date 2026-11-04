"use client";

import {
  BotIcon,
  CheckIcon,
  CircleSlashIcon,
  MessageSquareIcon,
  SquareIcon,
  XIcon,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import type { Agent } from "@/lib/api";
import { AgentsProvider, useAgents, useNow } from "@/lib/agents";
import { formatElapsed, formatRelative } from "@/lib/format";
import { cn } from "@/lib/utils";
import { chatHref, useAssistant } from "../assistant/assistant-provider";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Card } from "../ui/card";
import { Spinner } from "../ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";

/** Finished agents shown before "Show all". Working ones are always shown. */
const FINISHED_SHOWN = 4;

/**
 * Provides the user's agents to the app, going to an agent's chat when
 * asked to.
 */
export function AssistantAgents({ children }: { children: ReactNode }) {
  const router = useRouter();
  const { chatId } = useAssistant();

  return (
    <AgentsProvider watching={chatId} onOpenChat={(id) => router.push(chatHref(id))}>
      {children}
    </AgentsProvider>
  );
}

/**
 * Every coding agent the user has, from every chat, like a list of running
 * processes: what each is doing right now, for how long, and a way to stop
 * it. Hidden until the assistant has started one.
 */
export function AgentsSection() {
  const { agents, working } = useAgents();
  const router = useRouter();
  const [showAll, setShowAll] = useState(false);

  if (!agents || agents.length === 0) return null;

  const busy = agents.filter((agent) => agent.status === "working");
  const done = agents.filter((agent) => agent.status !== "working");
  const shown = [...busy, ...(showAll ? done : done.slice(0, FINISHED_SHOWN))];
  const hidden = done.length - (showAll ? done.length : Math.min(done.length, FINISHED_SHOWN));

  return (
    <section aria-labelledby="agents-heading" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 id="agents-heading" className="flex items-center gap-2 text-base font-semibold">
          Agents
          {working > 0 && (
            <Badge variant="secondary" className="gap-1.5 text-primary">
              <span className="size-1.5 animate-pulse rounded-full bg-primary" />
              {working} working
            </Badge>
          )}
        </h3>
        <p className="text-xs text-muted-foreground">
          Started by the assistant. Several can work on one machine at once.
        </p>
      </div>

      <Card className="gap-0 overflow-hidden py-0">
        <AgentList agents={shown} onOpenChat={(agent) => router.push(chatHref(agent.chat_id))} />
        {(hidden > 0 || showAll) && done.length > FINISHED_SHOWN && (
          <div className="border-t px-4 py-2">
            <Button variant="ghost" size="xs" onClick={() => setShowAll((all) => !all)}>
              {showAll ? "Show fewer" : `Show ${hidden} more`}
            </Button>
          </div>
        )}
      </Card>
    </section>
  );
}

/**
 * Agents as rows: what each is doing right now or how it ended, for how
 * long, and a way to stop it. `onOpenChat`, when given, adds a way to the
 * chat an agent belongs to.
 */
export function AgentList({
  agents,
  onOpenChat,
}: {
  agents: Agent[];
  onOpenChat?: (agent: Agent) => void;
}) {
  const { stop } = useAgents();
  const now = useNow();

  return (
    <ul aria-label="Agents" className="divide-y">
      {agents.map((agent) => (
        <AgentRow
          key={agent.id}
          agent={agent}
          now={now}
          onOpenChat={onOpenChat && (() => onOpenChat(agent))}
          onStop={() => stop(agent)}
        />
      ))}
    </ul>
  );
}

function AgentRow({
  agent,
  now,
  onOpenChat,
  onStop,
}: {
  agent: Agent;
  now: number;
  onOpenChat?: () => void;
  onStop: () => void;
}) {
  const working = agent.status === "working";

  return (
    <li
      aria-label={`${agent.handle} (${agent.name}) on ${agent.machine.name}`}
      className={cn("flex items-start gap-3 px-4 py-3", !working && "text-muted-foreground")}
    >
      <span className="mt-0.5 flex size-5 shrink-0 items-center justify-center">
        <AgentStatusIcon agent={agent} />
      </span>

      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm">
          <span className={cn("font-medium", working && "text-foreground")}>{agent.handle}</span>
          <span className="text-muted-foreground">{agent.name} on</span>
          <Link
            href={`/instances/${agent.machine.id}`}
            className="font-mono text-[13px] text-foreground underline-offset-4 hover:underline"
          >
            {agent.machine.name}
          </Link>
          {agent.background && working && (
            <Badge variant="outline" className="text-muted-foreground">
              Background
            </Badge>
          )}
        </p>

        {/* While working: what it is doing this moment. Otherwise: how it
            ended. The task itself is a line of context under either. */}
        {working ? (
          <p className="mt-1 truncate font-mono text-xs text-foreground" data-activity>
            {agent.activity || "Getting started"}
          </p>
        ) : (
          agent.report && (
            <p
              className={cn(
                "mt-1 line-clamp-2 text-xs",
                agent.status === "failed" && "text-destructive",
              )}
            >
              {agent.report.replace(/^Error: /, "")}
            </p>
          )
        )}
        {agent.task && (
          <p className="mt-1 truncate text-xs text-muted-foreground" title={agent.task}>
            {agent.task}
          </p>
        )}
      </div>

      <div className="flex shrink-0 items-center gap-1">
        <span className="mr-1 text-xs whitespace-nowrap text-muted-foreground tabular-nums">
          {working && agent.started_at
            ? formatElapsed(agent.started_at, now)
            : agent.finished_at && formatRelative(agent.finished_at, now)}
        </span>
        {onOpenChat && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Open the chat that started ${agent.handle}`}
                onClick={onOpenChat}
              >
                <MessageSquareIcon />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Open its chat</TooltipContent>
          </Tooltip>
        )}
        {working && (
          <Button variant="outline" size="sm" aria-label={`Stop ${agent.handle}`} onClick={onStop}>
            <SquareIcon className="fill-current" data-icon="inline-start" />
            Stop
          </Button>
        )}
      </div>
    </li>
  );
}

function AgentStatusIcon({ agent }: { agent: Agent }) {
  switch (agent.status) {
    case "working":
      return <Spinner className="text-primary" aria-label="Working" />;
    case "idle":
      return agent.finished_at ? (
        <CheckIcon className="size-4 text-success" aria-label="Finished" />
      ) : (
        <BotIcon className="size-4" aria-label="Idle" />
      );
    case "failed":
      return <XIcon className="size-4 text-destructive" aria-label="Failed" />;
    case "stopped":
      return <CircleSlashIcon className="size-4" aria-label="Stopped" />;
  }
}
