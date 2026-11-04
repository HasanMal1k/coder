"use client";

import { SquareTerminalIcon } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";
import type { Agent, AgentStatus, DesignNode } from "@/lib/api";
import { useAgents } from "@/lib/agents";
import { useArchitecture } from "@/lib/architecture";
import { ASSISTANT } from "@/lib/chat";
import { initials } from "@/lib/format";
import { displayStatus, useInstances } from "@/lib/instances";
import { cn } from "@/lib/utils";
import { TechLogo, techMark, type Mark } from "../architecture/tech";
import { useUser } from "../auth/require-auth";
import { StatusDot } from "../status";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";

/** How often the design is looked at for the agents' logos: they rarely change. */
const DESIGN_REFRESH_MS = 15000;

/** Someone in a chat the user can write to: the assistant, or one of its agents. */
export type Member = {
  /** What the user writes after an @. */
  handle: string;
  kind: "assistant" | "agent";
  /** An agent's job, such as "frontend". */
  role?: string;
  machine?: { id: string; name: string };
  status?: AgentStatus;
  /** The technology it builds, for its picture. */
  mark?: Mark;
};

const ASSISTANT_MEMBER: Member = { handle: ASSISTANT, kind: "assistant" };

/** The component an agent builds: the one named after it, or else the one on its machine. */
function componentOf(agent: Agent, nodes: DesignNode[]) {
  const name = agent.name.toLowerCase();
  return (
    nodes.find((node) => node.id === name) ??
    nodes.find((node) => node.name.toLowerCase() === name) ??
    nodes.find((node) => node.machine === agent.machine.name && node.kind !== "ci") ??
    nodes.find((node) => node.machine === agent.machine.name)
  );
}

/**
 * Who is in a chat: the assistant, then its agents by name,
 * each with what it is doing and the logo of what it builds.
 */
export function useChatPeople(chatId: string) {
  const { agents } = useAgents();
  const { architecture } = useArchitecture(chatId, DESIGN_REFRESH_MS);

  return useMemo(() => {
    const nodes = architecture?.design.nodes ?? [];
    const mine = (agents ?? [])
      .filter((agent) => agent.chat_id === chatId && agent.handle)
      .sort((a, b) => a.handle.localeCompare(b.handle));

    const members: Member[] = [ASSISTANT_MEMBER];
    for (const agent of mine) {
      const component = componentOf(agent, nodes);
      members.push({
        handle: agent.handle,
        kind: "agent",
        role: agent.name,
        machine: agent.machine,
        status: agent.status,
        mark: component ? techMark(component.tech) : undefined,
      });
    }
    return members;
  }, [agents, architecture, chatId]);
}

/** Who wrote a message, for its picture and its name. */
export type Author =
  { kind: "you" } | { kind: "assistant" } | { kind: "agent"; handle: string; member?: Member };

/**
 * A square picture beside a message or a name. The assistant's is the
 * product's terminal mark, light on dark; an agent's is the logo of what it
 * builds, or its initial. An orange dot says it is working right now.
 */
export function Avatar({ author, size = "md" }: { author: Author; size?: "sm" | "md" }) {
  const user = useUser();
  const small = size === "sm";
  const box = cn(
    "relative grid shrink-0 place-items-center rounded-md select-none",
    small ? "size-5 text-[10px]" : "size-8 text-xs",
  );

  if (author.kind === "you") {
    return (
      <span aria-hidden className={cn(box, "bg-secondary font-medium text-muted-foreground")}>
        {initials(user.display_name, user.email)}
      </span>
    );
  }

  if (author.kind === "assistant") {
    return (
      <span aria-hidden className={cn(box, "bg-foreground text-background")}>
        <SquareTerminalIcon className={small ? "size-3" : "size-4"} />
      </span>
    );
  }

  const member = author.member;
  return (
    <span aria-hidden className={cn(box, "bg-secondary font-semibold text-foreground")}>
      {member?.mark ? (
        <TechLogo mark={member.mark} className={small ? "size-3" : "size-4"} />
      ) : (
        author.handle.slice(0, 1).toUpperCase()
      )}
      {member?.status === "working" && (
        <span
          className={cn(
            "absolute rounded-full bg-primary ring-2 ring-background",
            small ? "-right-0.5 -bottom-0.5 size-1.5" : "-right-0.5 -bottom-0.5 size-2.5",
          )}
        />
      )}
    </span>
  );
}

/** What an agent is, in a few words: "frontend on web". */
export function roleText(role?: string, machine?: string) {
  if (role && machine) return `${role} on ${machine}`;
  return role ?? machine ?? "";
}

/**
 * The people in a chat, and its machines: a row under the title. Choosing a
 * person starts a message to them.
 */
export function ChatPeople({
  members,
  machines,
  onMention,
}: {
  members: Member[];
  machines: { id: string; name: string }[];
  onMention: (handle: string) => void;
}) {
  const { instances, pending } = useInstances();

  // Only machines that still exist: one deleted since has nothing to show.
  const live = machines.flatMap((machine) => {
    const instance = instances?.find((i) => i.id === machine.id);
    return instance ? [instance] : [];
  });

  // A chat with nobody but the assistant has no one to introduce.
  if (members.length < 2 && live.length === 0) return null;

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1.5 px-3 pb-2 sm:px-4">
      <ul aria-label="People in this chat" className="flex flex-wrap items-center gap-1">
        {members.map((member) => (
          <li key={member.handle}>
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => onMention(member.handle)}
                  aria-label={`Write to @${member.handle}`}
                  className="flex items-center gap-1.5 rounded-md py-1 pr-2.5 pl-1 text-xs transition-colors outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/50"
                >
                  <Avatar
                    size="sm"
                    author={
                      member.kind === "assistant"
                        ? { kind: "assistant" }
                        : { kind: "agent", handle: member.handle, member }
                    }
                  />
                  <span className="font-medium">
                    {member.kind === "assistant" ? "Assistant" : member.handle}
                  </span>
                </button>
              </TooltipTrigger>
              <TooltipContent>
                {member.kind === "assistant"
                  ? "Runs the project. Messages go here unless you @ someone."
                  : `${roleText(member.role, member.machine?.name)}, ${
                      member.status === "working" ? "working" : "free"
                    }. Write @${member.handle} to talk to it directly.`}
              </TooltipContent>
            </Tooltip>
          </li>
        ))}
      </ul>

      {live.length > 0 && (
        <ul aria-label="Machines in this chat" className="flex flex-wrap items-center gap-1">
          {live.map((instance) => (
            <li key={instance.id}>
              <Link
                href={`/instances/${instance.id}`}
                className="flex items-center gap-2 rounded-md py-1 pr-2.5 pl-2 text-xs text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
              >
                <StatusDot status={displayStatus(instance, pending[instance.id])} />
                <span className="font-mono">{instance.name}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
