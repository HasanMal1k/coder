import { useCallback, useEffect, useReducer, useState } from "react";
import {
  ApiError,
  listChats,
  streamChat,
  type Chat,
  type ChatEvent,
  type AgentExchange,
  type AgentNews,
  type AgentPost,
  type Attachment,
  type ToolCall,
  type ToolResult,
} from "./api";
import { usePolling } from "./use-polling";

const RECONNECT_MIN_MS = 1000;
const RECONNECT_MAX_MS = 10000;
const CHATS_REFRESH_MS = 15000;

type ChatState = {
  /** null until the server has said what the chat is. */
  chat: Chat | null;
  events: ChatEvent[];
  /** The chat doesn't exist, or was deleted while open. */
  missing: boolean;
  /** The connection dropped and is being retried. */
  offline: boolean;
  /** The message the assistant is writing right now, as far as it has got. */
  draft: string;
};

type ChatAction =
  | { type: "event"; event: ChatEvent }
  | { type: "chat"; chat: Chat }
  | { type: "draft"; text: string }
  | { type: "missing" }
  | { type: "offline"; offline: boolean };

const INITIAL: ChatState = {
  chat: null,
  events: [],
  missing: false,
  offline: false,
  draft: "",
};

function reduce(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "event": {
      // A reconnect can replay an entry that already arrived.
      const last = state.events[state.events.length - 1];
      if (last && action.event.seq <= last.seq) return state;
      return { ...state, events: [...state.events, action.event] };
    }
    case "chat":
      return { ...state, chat: action.chat, offline: false };
    case "draft":
      return state.draft === action.text ? state : { ...state, draft: action.text };
    case "missing":
      return { ...state, missing: true };
    case "offline":
      return state.offline === action.offline ? state : { ...state, offline: action.offline };
  }
}

/**
 * A chat and its history, kept up to date for as long as the component is
 * mounted. The connection is retried whenever it drops, picking up after the
 * last entry seen. Remount (change `key`) to follow a different chat.
 */
export function useChat(id: string) {
  const [state, dispatch] = useReducer(reduce, INITIAL);

  useEffect(() => {
    const abort = new AbortController();

    (async () => {
      let after = 0;
      let delay = RECONNECT_MIN_MS;

      while (!abort.signal.aborted) {
        let gone = false;

        try {
          await streamChat(
            id,
            after,
            {
              event: (event) => {
                after = event.seq;
                delay = RECONNECT_MIN_MS;
                dispatch({ type: "event", event });
              },
              chat: (chat) => dispatch({ type: "chat", chat }),
              draft: (text) => dispatch({ type: "draft", text }),
              deleted: () => {
                gone = true;
              },
            },
            abort.signal,
          );
        } catch (err) {
          if (abort.signal.aborted) return;
          // Signed out: the app is about to leave this page.
          if (err instanceof ApiError && err.status === 401) return;
          if (err instanceof ApiError && err.status === 404) gone = true;
        }

        if (abort.signal.aborted) return;
        if (gone) {
          dispatch({ type: "missing" });
          return;
        }

        // Whatever was being written when the connection went is stale.
        dispatch({ type: "draft", text: "" });
        dispatch({ type: "offline", offline: true });
        await new Promise((resolve) => setTimeout(resolve, delay));
        delay = Math.min(delay * 2, RECONNECT_MAX_MS);
      }
    })();

    return () => abort.abort();
  }, [id]);

  return state;
}

/** The user's chats, most recent first; null until loaded. */
export function useChats() {
  const [chats, setChats] = useState<Chat[] | null>(null);

  const refresh = useCallback(async () => {
    try {
      setChats(await listChats());
    } catch {
      // The list is a convenience; the next refresh will try again.
    }
  }, []);

  usePolling(refresh, CHATS_REFRESH_MS);

  return { chats, refresh };
}

/** How the user addresses the assistant itself. */
export const ASSISTANT = "assistant";

/** One thing the assistant set out to do, and how it went. */
export type Step = {
  call: ToolCall;
  name: string;
  args: Record<string, unknown>;
  /** Absent until the step finishes. */
  result?: ToolResult;
  /** What has happened so far while the step runs, oldest first. */
  activity: string[];
  /** For a task given to a background agent: how it ended, once it has. */
  news?: AgentNews;
};

/** A machine a chat has worked with. */
export type ChatMachine = { id: string; name: string };

/** The machines a chat has created or worked on, in the order it met them. */
export function chatMachines(items: TranscriptItem[]): ChatMachine[] {
  const seen = new Map<string, ChatMachine>();

  for (const item of items) {
    if (item.type !== "assistant") continue;
    for (const step of item.steps) {
      const machine = step.result?.machine;
      if (machine && !seen.has(machine.id)) seen.set(machine.id, machine);
    }
  }

  return [...seen.values()];
}

/**
 * A number that changes whenever a chat's history says an agent started,
 * finished or was stopped, so the list of agents can be looked at again.
 */
export function agentChanges(events: ChatEvent[]) {
  const coding = new Set<string>();
  const begun = new Set<string>();
  let changes = 0;

  for (const event of events) {
    if (event.kind === "assistant") {
      for (const call of event.payload.tool_calls ?? []) {
        if (call.function.name === "code_on_machine") coding.add(call.id);
      }
    } else if (event.kind === "activity" && coding.has(event.payload.tool_call_id)) {
      // The first sign of an agent at work is the first thing it does.
      begun.add(event.payload.tool_call_id);
    } else if (
      event.kind === "tool" &&
      ["code_on_machine", "stop_agent"].includes(event.payload.name)
    ) {
      changes++;
    } else if (event.kind === "agent" || event.kind === "post") {
      changes++;
    } else if (event.kind === "user" && event.payload.to?.length) {
      // Written to agents: they start work on it.
      changes++;
    }
  }

  return changes + begun.size;
}

/** The steps that change which machines exist or whether they are running. */
const LIFECYCLE = ["create_machine", "start_machine", "stop_machine"];

/**
 * A number that changes whenever a chat's history says something happened to
 * a machine, so the rest of the app knows to look at its machines again.
 */
export function machineChanges(events: ChatEvent[]) {
  const lifecycle = new Set<string>();
  let changes = 0;

  for (const event of events) {
    if (event.kind === "assistant") {
      for (const call of event.payload.tool_calls ?? []) {
        if (LIFECYCLE.includes(call.function.name)) lifecycle.add(call.id);
      }
    } else if (event.kind === "tool" && event.payload.machine) {
      changes++;
    } else if (event.kind === "activity" && lifecycle.has(event.payload.tool_call_id)) {
      changes++;
    }
  }

  return changes;
}

/** An agent the user wrote to, and how far it has got with answering. */
export type Answering = {
  handle: string;
  /** What it has done so far, oldest first. */
  activity: string[];
  /** It has answered (or said it could not). */
  answered: boolean;
  /** It was busy, and reads the message when its task ends. */
  queued: boolean;
};

export type TranscriptItem = { seq: number; at: string } & (
  | {
      type: "user";
      text: string;
      /** The agents it was written to, by handle; "assistant" if named too. */
      to: string[];
      attachments: Attachment[];
      /** The agents it was written to, each with how it is getting on. */
      answering: Answering[];
    }
  | { type: "assistant"; text: string; steps: Step[] }
  | { type: "error"; message: string; stopped: boolean }
  /** A background agent finished, at this point in the conversation. */
  | { type: "news"; news: AgentNews }
  /** One agent asked another something. */
  | { type: "exchange"; exchange: AgentExchange }
  /**
   * An agent wrote in the chat. `quote` is what the user wrote that it
   * answers; `answer` is what the user chose, for a question they have
   * answered.
   */
  | { type: "post"; post: AgentPost; quote?: string; answer?: string }
);

/** The ID an agent's work on the user's message is recorded under (see the server's directCallID). */
export function directCallId(seq: number, handle: string) {
  return `direct-${seq}-${handle}`;
}

function parseArgs(raw: string): Record<string, unknown> {
  try {
    const value: unknown = JSON.parse(raw || "{}");
    return value && typeof value === "object" ? (value as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/**
 * Arranges a chat's history for display: each of the assistant's messages
 * with the steps it took, each step with its progress and its outcome.
 */
export function buildTranscript(events: ChatEvent[]): TranscriptItem[] {
  const items: TranscriptItem[] = [];
  const steps = new Map<string, Step>();
  // The user's messages to agents, by sequence number, and each agent's
  // progress on one, by the ID its work is recorded under.
  const written = new Map<number, Extract<TranscriptItem, { type: "user" }>>();
  const answering = new Map<string, Answering>();
  // Questions from agents still open, by the agent asking.
  const asking = new Map<string, Extract<TranscriptItem, { type: "post" }>>();

  for (const event of events) {
    const at = event.created_at;

    switch (event.kind) {
      case "user": {
        const to = event.payload.to ?? [];
        const item = {
          type: "user" as const,
          seq: event.seq,
          at,
          text: event.payload.text,
          to,
          attachments: event.payload.attachments ?? [],
          answering: to
            .filter((handle) => handle !== ASSISTANT)
            .map((handle) => ({ handle, activity: [], answered: false, queued: false })),
        };
        for (const one of item.answering) answering.set(directCallId(event.seq, one.handle), one);
        written.set(event.seq, item);
        // Writing to an agent that asked something is the answer to it.
        for (const handle of to) {
          const question = asking.get(handle);
          if (question) {
            question.answer = event.payload.text;
            asking.delete(handle);
          }
        }
        items.push(item);
        break;
      }

      case "post": {
        const post = event.payload;
        const asked = post.reply_to ? written.get(post.reply_to) : undefined;
        const one = asked?.answering.find((a) => a.handle === post.agent);
        if (one) {
          if (post.queued) one.queued = true;
          else one.answered = true;
        }
        // Word that it is busy is shown with the message, not by itself.
        if (post.queued) break;

        const item = { type: "post" as const, seq: event.seq, at, post, quote: asked?.text };
        if (post.question) asking.set(post.agent, item);
        items.push(item);
        break;
      }

      case "assistant": {
        const list: Step[] = (event.payload.tool_calls ?? []).map((call) => ({
          call,
          name: call.function.name,
          args: parseArgs(call.function.arguments),
          activity: [],
        }));
        for (const step of list) steps.set(step.call.id, step);
        items.push({
          type: "assistant",
          seq: event.seq,
          at,
          text: event.payload.text,
          steps: list,
        });
        break;
      }

      case "tool": {
        // The answer to a question is a result like any other: it is
        // shown with the question it answers.
        const step = steps.get(event.payload.tool_call_id);
        if (step) step.result = event.payload;
        break;
      }

      case "activity":
        steps.get(event.payload.tool_call_id)?.activity.push(event.payload.text);
        answering.get(event.payload.tool_call_id)?.activity.push(event.payload.text);
        break;

      case "agent": {
        // Shown twice: on the task where it started, which now has its
        // outcome, and here, where it happened, which is why the assistant
        // speaks up next.
        const step = steps.get(event.payload.tool_call_id);
        if (step) step.news = event.payload;
        items.push({ type: "news", seq: event.seq, at, news: event.payload });
        break;
      }

      case "message":
        items.push({ type: "exchange", seq: event.seq, at, exchange: event.payload });
        break;

      case "error":
        items.push({
          type: "error",
          seq: event.seq,
          at,
          message: event.payload.message,
          stopped: event.payload.stopped ?? false,
        });
        break;
    }
  }

  return items;
}

/** "@name" in what the user writes, as the server finds it (see its handles.go). */
const MENTION = /(?:^|[^\w@.])@([a-z][a-z0-9-]*)/gi;

/**
 * Who a message will go to, as the server will decide it: the agents among
 * `handles` it mentions, and whether the assistant gets it too (it does when
 * it is named, or when no agent is).
 */
export function recipientsOf(text: string, handles: string[]) {
  const agents: string[] = [];
  let assistant = false;

  for (const match of text.toLowerCase().matchAll(MENTION)) {
    const name = match[1].replace(/-+$/, "");
    if (name === ASSISTANT) assistant = true;
    else if (handles.includes(name) && !agents.includes(name)) agents.push(name);
  }

  return { agents, assistant: assistant || agents.length === 0 };
}
