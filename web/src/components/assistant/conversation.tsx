"use client";

import {
  MessageSquareDashedIcon,
  SparklesIcon,
  TriangleAlertIcon,
  WifiOffIcon,
} from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import {
  approveArchitecture,
  cancelChat,
  createChat,
  deleteChat,
  errorMessage,
  sendMessage,
  type Chat,
} from "@/lib/api";
import { useAgents } from "@/lib/agents";
import { agentChanges, buildTranscript, chatMachines, machineChanges, useChat } from "@/lib/chat";
import { displayStatus, useInstances } from "@/lib/instances";
import { cn } from "@/lib/utils";
import { StatusDot } from "../status";
import { Alert, AlertDescription, AlertTitle } from "../ui/alert";
import { Skeleton } from "../ui/skeleton";
import { useAssistant } from "./assistant-provider";
import { Composer } from "./composer";
import { Transcript } from "./transcript";

/** How close to the bottom counts as "reading the latest", in pixels. */
const STICK_DISTANCE = 120;

const SUGGESTIONS = [
  "Build a website with a backend and a frontend",
  "Set up a Python project with a test that passes",
  "Make a Node.js server that says hello, and show it to me",
];

/** Shown when the server has no model to talk to. */
function NotSetUp() {
  return (
    <Alert>
      <TriangleAlertIcon />
      <AlertTitle>The assistant isn&apos;t set up on this server</AlertTitle>
      <AlertDescription>
        It needs a key for its model. Add <code className="font-mono">DEEPSEEK_API_KEY</code> to the
        server&apos;s <code className="font-mono">.env</code> file and restart the server.
      </AlertDescription>
    </Alert>
  );
}

/**
 * The start of a conversation: what the assistant is for, somewhere to type,
 * and a few things to try. `onStarted` is given the chat once its first
 * message has been accepted.
 */
export function NewConversation({
  compact,
  onStarted,
}: {
  /** With the design beside it: less room, so less ceremony. */
  compact: boolean;
  onStarted: (chat: Chat) => void;
}) {
  const { info, refreshChats } = useAssistant();
  const [draft, setDraft] = useState("");
  const [starting, setStarting] = useState(false);

  async function start() {
    const text = draft.trim();
    if (!text) return;

    setStarting(true);
    let created: Chat | null = null;
    try {
      created = await createChat();
      await sendMessage(created.id, text);
      refreshChats();
      onStarted(created);
    } catch (err) {
      // Don't leave an empty chat behind for a message that never arrived.
      if (created) deleteChat(created.id).catch(() => {});
      toast.error("Couldn't start the chat", { description: errorMessage(err) });
      setStarting(false);
    }
  }

  const disabled = info?.enabled === false || starting;

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div
        className={cn(
          "mx-auto flex min-h-full w-full max-w-3xl flex-col justify-center",
          compact ? "gap-6 p-4" : "gap-8 p-4 sm:p-6 lg:p-8",
        )}
      >
        <div className="text-center">
          <div
            className={cn(
              "mx-auto flex items-center justify-center rounded-lg border bg-card text-foreground",
              compact ? "size-10" : "size-12",
            )}
          >
            <SparklesIcon className={compact ? "size-5" : "size-6"} />
          </div>
          <h2 className={cn("mt-4 font-semibold tracking-tight", compact ? "text-lg" : "text-2xl")}>
            What should we build?
          </h2>
          <p className="mx-auto mt-2 max-w-lg text-sm text-balance text-muted-foreground">
            Say it in plain words. The assistant asks what it needs to know, creates the machines,
            and writes and runs the code on them while you watch.
          </p>
        </div>

        {info?.enabled === false && <NotSetUp />}

        <Composer
          value={draft}
          onChange={setDraft}
          onSend={start}
          onStop={() => {}}
          working={false}
          disabled={disabled}
          placeholder="A website with a backend and a frontend…"
        />

        <div className="flex flex-col gap-2">
          <p className="text-xs font-medium text-muted-foreground">Or start from one of these</p>
          {SUGGESTIONS.map((suggestion) => (
            <button
              key={suggestion}
              type="button"
              disabled={disabled}
              onClick={() => setDraft(suggestion)}
              className="rounded-lg border bg-card px-4 py-3 text-left text-sm transition-colors outline-none hover:border-foreground/40 hover:bg-muted focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50"
            >
              {suggestion}
            </button>
          ))}
        </div>

        {info?.model && (
          <p className="text-center text-xs text-muted-foreground">
            Uses {info.model}, with OpenCode doing the coding inside each machine.
          </p>
        )}
      </div>
    </div>
  );
}

/**
 * One chat, live: its history, what the assistant is doing right now, and
 * somewhere to answer. Give it a `key` when the chat can change, so each
 * starts from a clean slate. `onMissing` is called if the chat turns out not
 * to exist.
 */
export function Conversation({
  id,
  compact,
  onLoaded,
  onMissing,
}: {
  id: string;
  compact: boolean;
  /** Called with the chat whenever its title or status changes. */
  onLoaded?: (chat: Chat) => void;
  onMissing?: () => void;
}) {
  const { info, refreshChats } = useAssistant();
  const { chat, events, missing, offline, draft: writing } = useChat(id);
  const { refresh: refreshMachines } = useInstances();

  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);

  const items = useMemo(() => buildTranscript(events), [events]);

  // Stay at the bottom as the conversation grows, unless the user has
  // scrolled up to read something.
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  useEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTo({ top: el.scrollHeight });
  }, [events.length, chat?.status, writing]);

  // The assistant creates, starts and stops machines. The design beside
  // this should show that as it happens, not at its next refresh.
  const changes = useMemo(() => machineChanges(events), [events]);
  useEffect(() => {
    if (changes > 0) refreshMachines();
  }, [changes, refreshMachines]);

  // Likewise the list of agents, as they start and finish.
  const { refresh: refreshAgents } = useAgents();
  const agentsChanged = useMemo(() => agentChanges(events), [events]);
  useEffect(() => {
    if (agentsChanged > 0) refreshAgents();
  }, [agentsChanged, refreshAgents]);

  // Keeps the list of chats (its titles, and which are busy) in step.
  useEffect(() => {
    if (!chat) return;
    refreshChats();
    onLoaded?.(chat);
  }, [chat, refreshChats, onLoaded]);

  useEffect(() => {
    if (missing) onMissing?.();
  }, [missing, onMissing]);

  async function send(text: string, fromDraft: boolean) {
    setSending(true);
    try {
      await sendMessage(id, text);
      if (fromDraft) setDraft("");
      stick.current = true;
    } catch (err) {
      toast.error("Couldn't send your message", { description: errorMessage(err) });
    } finally {
      setSending(false);
    }
  }

  async function approve() {
    try {
      await approveArchitecture(id);
      stick.current = true;
    } catch (err) {
      toast.error("Couldn't approve the design", { description: errorMessage(err) });
    }
  }

  async function stop() {
    try {
      await cancelChat(id);
    } catch (err) {
      toast.error("Couldn't stop the assistant", { description: errorMessage(err) });
    }
  }

  if (missing) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
        <div className="flex size-10 items-center justify-center rounded-lg bg-muted">
          <MessageSquareDashedIcon className="size-6" />
        </div>
        <p className="font-medium">This chat doesn&apos;t exist</p>
        <p className="text-sm text-muted-foreground">It may have been deleted.</p>
      </div>
    );
  }

  const status = chat?.status ?? "idle";
  const working = status === "running" || sending;

  return (
    <>
      <ChatMachines items={items} />

      <div
        ref={scroller}
        onScroll={(event) => {
          const el = event.currentTarget;
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < STICK_DISTANCE;
        }}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        <div className={cn("mx-auto w-full max-w-3xl", compact ? "p-4" : "p-4 pb-8 sm:p-6 lg:p-8")}>
          {chat === null ? (
            <div className="space-y-6" aria-busy="true" aria-label="Loading the chat">
              <Skeleton className="ml-auto h-10 w-2/3 rounded-xl" />
              <Skeleton className="h-20 w-5/6 rounded-lg" />
              <Skeleton className="h-10 w-1/2 rounded-lg" />
            </div>
          ) : (
            <Transcript
              items={items}
              status={status}
              draft={writing}
              compact={compact}
              onAnswer={(answer) => send(answer, false)}
              onApprove={approve}
            />
          )}
        </div>
      </div>

      <div className="shrink-0 border-t bg-background/80 backdrop-blur">
        <div
          className={cn(
            "mx-auto flex w-full max-w-3xl flex-col gap-3",
            compact ? "p-3" : "p-4 sm:px-6 lg:px-8",
          )}
        >
          {offline && (
            <p className="flex items-center gap-2 text-xs text-warning" role="status">
              <WifiOffIcon className="size-3.5" />
              Lost the connection. Reconnecting; nothing is lost.
            </p>
          )}
          {info?.enabled === false && <NotSetUp />}

          <Composer
            value={draft}
            onChange={setDraft}
            onSend={() => send(draft.trim(), true)}
            onStop={stop}
            working={working}
            disabled={chat === null || info?.enabled === false}
            placeholder={
              status === "waiting"
                ? "Answer, or say what to change…"
                : "Ask for something to be built or changed…"
            }
          />
        </div>
      </div>
    </>
  );
}

/**
 * The machines this chat has made or worked on, with whether each is running
 * right now: the state of the world next to the story of how it got there.
 */
function ChatMachines({ items }: { items: ReturnType<typeof buildTranscript> }) {
  const { instances, pending } = useInstances();

  // Only machines that still exist: one deleted since has nothing to show.
  const machines = chatMachines(items).flatMap((machine) => {
    const instance = instances?.find((i) => i.id === machine.id);
    return instance ? [instance] : [];
  });

  if (machines.length === 0) return null;

  return (
    <ul
      aria-label="Machines in this chat"
      className="flex shrink-0 flex-wrap items-center gap-1.5 border-b px-3 py-2"
    >
      {machines.map((instance) => (
        <li key={instance.id}>
          <Link
            href={`/instances/${instance.id}`}
            className="flex items-center gap-2 rounded-md border bg-card py-1 pr-3 pl-2.5 text-xs transition-colors outline-none hover:border-foreground/40 hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <StatusDot status={displayStatus(instance, pending[instance.id])} />
            <span className="font-mono">{instance.name}</span>
          </Link>
        </li>
      ))}
    </ul>
  );
}

/** Deletes a chat, saying so. Resolves to whether it was deleted. */
export async function removeChat(id: string) {
  try {
    await deleteChat(id);
    toast.success("Chat deleted. Machines it made are still there.");
    return true;
  } catch (err) {
    toast.error("Couldn't delete the chat", { description: errorMessage(err) });
    return false;
  }
}
