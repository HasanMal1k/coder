"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { toast } from "sonner";
import { errorMessage, listAgents, stopAgent, type Agent } from "./api";
import { usePolling } from "./use-polling";

/** How often to look while any agent is working, and while none is. */
const BUSY_REFRESH_MS = 2000;
const QUIET_REFRESH_MS = 8000;

/** Tells the user an agent has finished, wherever they are in the app. */
function announce(agent: Agent, openChat: (chatId: string) => void) {
  const where = `${agent.name} on ${agent.machine.name}`;
  const action = { label: "Open chat", onClick: () => openChat(agent.chat_id) };

  if (agent.status === "idle") toast.success(`${where} finished`, { action });
  else if (agent.status === "failed")
    toast.error(`${where} failed`, { description: agent.report, action });
  // A stop is something the user did; telling them about it is noise.
}

type AgentsState = {
  /** The user's agents, those working first; null until loaded. */
  agents: Agent[] | null;
  /** How many are working right now. */
  working: number;
  refresh: () => Promise<void>;
  stop: (agent: Agent) => Promise<void>;
};

const AgentsContext = createContext<AgentsState | null>(null);

/**
 * The user's coding agents, for every page: what each is doing, kept up to
 * date often enough to read as live while any of them is working. When one
 * finishes, the user is told wherever they are in the app.
 *
 * `onOpenChat` opens the chat an agent belongs to. `watching` is the chat the
 * user is looking at, if any: they can see its agents finish for themselves,
 * so nothing is announced for those.
 */
export function AgentsProvider({
  onOpenChat,
  watching,
  children,
}: {
  onOpenChat: (chatId: string) => void;
  watching: string | null;
  children: ReactNode;
}) {
  const [agents, setAgents] = useState<Agent[] | null>(null);

  // What each agent was doing at the last look, to notice one finishing.
  // Empty until the first look, so agents that finished before the app was
  // opened aren't announced.
  const previous = useRef<Map<string, Agent> | null>(null);
  const openChat = useRef(onOpenChat);
  const watched = useRef(watching);
  useEffect(() => {
    openChat.current = onOpenChat;
    watched.current = watching;
  }, [onOpenChat, watching]);

  const refresh = useCallback(async () => {
    let list: Agent[];
    try {
      list = await listAgents();
    } catch {
      // The list is kept as it was; the next look will try again.
      return;
    }

    const before = previous.current;
    if (before) {
      for (const agent of list) {
        const was = before.get(agent.id);
        const finished = was?.status === "working" && agent.status !== "working";
        if (finished && agent.chat_id !== watched.current) {
          announce(agent, (chatId) => openChat.current(chatId));
        }
      }
    }
    previous.current = new Map(list.map((agent) => [agent.id, agent]));
    setAgents(list);
  }, []);

  const working = agents?.filter((agent) => agent.status === "working").length ?? 0;
  usePolling(refresh, working > 0 ? BUSY_REFRESH_MS : QUIET_REFRESH_MS);

  const stop = useCallback(
    async (agent: Agent) => {
      try {
        await stopAgent(agent.id);
      } catch (err) {
        toast.error(`Couldn't stop ${agent.name}`, { description: errorMessage(err) });
      } finally {
        refresh();
      }
    },
    [refresh],
  );

  const value = useMemo(
    () => ({ agents, working, refresh, stop }),
    [agents, working, refresh, stop],
  );

  return <AgentsContext.Provider value={value}>{children}</AgentsContext.Provider>;
}

export function useAgents() {
  const state = useContext(AgentsContext);
  if (!state) throw new Error("useAgents must be used inside AgentsProvider");
  return state;
}

/** The current time, updated every `intervalMs`, for showing how long things take. */
export function useNow(intervalMs = 1000) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(timer);
  }, [intervalMs]);

  return now;
}
