"use client";

import { MessageSquareDashedIcon, TriangleAlertIcon, WifiOffIcon } from "lucide-react";
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
export function NewConversation({ onStarted }: { onStarted: (chat: Chat) => void }) {
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
      {/* A little above the middle, where the eye rests on an empty page. */}
      <div className="mx-auto flex min-h-full w-full max-w-2xl flex-col justify-center gap-6 px-4 pt-8 pb-[12vh] sm:px-6">
        <div className="text-center">
          <h2 className="text-3xl font-semibold tracking-tight @2xl/page:text-4xl">
            What should we build?
          </h2>
          <p className="mx-auto mt-3 max-w-md text-sm text-pretty text-muted-foreground">
            Describe it in plain words. The assistant designs it with you, then builds it on your
            machines while you watch.
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
          roomy
          note={
            info?.model && (
              <>
                <span className="font-mono" translate="no">
                  {info.model}
                </span>
                , with OpenCode writing the code
              </>
            )
          }
        />

        {/* Things to try, as quiet as the page: each fills the box when chosen. */}
        <ul aria-label="Things to try" className="flex flex-wrap justify-center gap-2">
          {SUGGESTIONS.map((suggestion) => (
            <li key={suggestion}>
              <button
                type="button"
                disabled={disabled}
                onClick={() => setDraft(suggestion)}
                className="rounded-lg bg-card px-3 py-2 text-left text-sm text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50"
              >
                {suggestion}
              </button>
            </li>
          ))}
        </ul>
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

  // The same when the room changes and the words don't: the row of machines
  // arriving above, a long answer being typed below, the window resized. Any
  // of them would otherwise leave the last message under the edge.
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;

    const observer = new ResizeObserver(() => {
      if (stick.current) el.scrollTo({ top: el.scrollHeight });
    });
    observer.observe(el);
    if (el.firstElementChild) observer.observe(el.firstElementChild);
    return () => observer.disconnect();
  }, []);

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
        <div className="flex size-10 items-center justify-center rounded-lg border text-muted-foreground">
          <MessageSquareDashedIcon className="size-5" />
        </div>
        <p className="font-medium">This chat doesn&apos;t exist</p>
        <p className="text-sm text-muted-foreground">
          It may have been deleted.{" "}
          <Link href="/" className="font-medium text-foreground underline underline-offset-4">
            Start a new chat
          </Link>
        </p>
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
        // With no line under the header, what scrolls up fades out beneath it.
        className="min-h-0 flex-1 overflow-y-auto [mask-image:linear-gradient(to_bottom,transparent,black_1rem)]"
      >
        <div
          className={cn(
            "mx-auto w-full max-w-3xl pb-10",
            compact ? "px-4 pt-4" : "px-4 pt-4 sm:px-6 sm:pt-6 lg:px-8 lg:pt-8",
          )}
        >
          {chat === null ? (
            <div className="space-y-6" aria-busy="true" aria-label="Loading the chat">
              <Skeleton className="ml-auto h-10 w-2/3 rounded-lg" />
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

      {/* No line above it: the conversation fades out behind where you type. */}
      <div className="relative shrink-0 before:pointer-events-none before:absolute before:inset-x-0 before:bottom-full before:h-8 before:bg-linear-to-t before:from-background">
        <div
          className={cn(
            "mx-auto flex w-full max-w-3xl flex-col gap-3 pb-[max(--spacing(4),env(safe-area-inset-bottom))]",
            compact ? "px-3" : "px-4 sm:px-6 lg:px-8",
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
              status === "waiting" ? "Answer, or say what to change…" : "Build or change something…"
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
      className="flex shrink-0 flex-wrap items-center gap-1.5 px-3 pb-2 sm:px-4"
    >
      {machines.map((instance) => (
        <li key={instance.id}>
          <Link
            href={`/instances/${instance.id}`}
            className="flex items-center gap-2 rounded-md bg-card py-1 pr-3 pl-2.5 text-xs transition-colors outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/50"
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
