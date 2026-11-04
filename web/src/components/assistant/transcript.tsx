"use client";

import {
  CheckIcon,
  ChevronRightIcon,
  CircleSlashIcon,
  CornerDownRightIcon,
  ExternalLinkIcon,
  MessageCircleQuestionIcon,
  MessagesSquareIcon,
  NetworkIcon,
  SquareTerminalIcon,
  TriangleAlertIcon,
  XIcon,
} from "lucide-react";
import Link from "next/link";
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import {
  errorMessage,
  portUrl,
  type AgentExchange,
  type AgentNews,
  type AgentPost,
  type ChatStatus,
  type DesignNode,
} from "@/lib/api";
import { ASSISTANT, type Answering, type Step, type TranscriptItem } from "@/lib/chat";
import { useNow } from "@/lib/agents";
import { formatDateTime, formatMemory } from "@/lib/format";
import { cn } from "@/lib/utils";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Spinner } from "../ui/spinner";
import { useAssistant } from "./assistant-provider";
import { AttachmentList } from "./attachments";
import { Markdown } from "./markdown";
import { Avatar, roleText, type Author, type Member } from "./people";

/** Messages from one author closer together than this read as one. */
const GROUP_MS = 5 * 60 * 1000;
/** How long after the user writes to an agent its silence still means "starting". */
const STARTING_MS = 30 * 1000;

/** The chat and its people, for the parts of a message that need them. */
type Room = {
  chatId: string;
  members: Member[];
  /** Answers an agent's question, or writes to it. */
  onWriteTo: (handle: string, text: string) => void;
};

const RoomContext = createContext<Room>({ chatId: "", members: [], onWriteTo: () => {} });

function useRoom() {
  return useContext(RoomContext);
}

/** The member with a handle, or the agent with a job on a machine. */
function findMember(members: Member[], handle: string) {
  return members.find((m) => m.handle === handle);
}

function memberFor(members: Member[], role: string, machine: string) {
  return members.find((m) => m.role === role && m.machine?.name === machine);
}

/** "13:14", in the reader's own clock. */
function clock(iso: string) {
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(
    new Date(iso),
  );
}

/** How a step stands: still going, finished, failed, or cut short. */
type Outcome = "working" | "done" | "failed" | "interrupted";

function outcomeOf(step: Step, status: ChatStatus): Outcome {
  // A background task's result only says it started; how it went arrives
  // later, whatever the chat is doing meanwhile.
  if (isBackground(step) && step.result && !step.result.failed) {
    if (!step.news) return "working";
    if (step.news.stopped) return "interrupted";
    return step.news.failed ? "failed" : "done";
  }

  if (step.result) return step.result.failed ? "failed" : "done";
  // No result: either it is still running, or the run ended without it.
  return status === "running" ? "working" : "interrupted";
}

function isBackground(step: Step) {
  return step.name === "code_on_machine" && step.args.background === true;
}

/** The agent a coding task was given to. */
function agentOf(step: Step) {
  return text(step.args.agent) || "coder";
}

function text(value: unknown) {
  return typeof value === "string" ? value : "";
}

/** Who wrote an item, as a key: items in a row by one author are grouped. */
function authorOf(item: TranscriptItem): Author | null {
  switch (item.type) {
    case "user":
      return { kind: "you" };
    case "assistant":
    case "error":
      return { kind: "assistant" };
    case "post":
      return { kind: "agent", handle: item.post.agent };
    default:
      // News and exchanges are the platform's, between messages.
      return null;
  }
}

function sameAuthor(a: Author | null, b: Author | null) {
  if (!a || !b || a.kind !== b.kind) return false;
  return a.kind !== "agent" || (b.kind === "agent" && a.handle === b.handle);
}

/**
 * The conversation, as a channel: everything the user, the assistant and the
 * agents said, each with who said it and when, and what the assistant did.
 */
export function Transcript({
  chatId,
  items,
  status,
  draft,
  compact,
  members,
  onAnswer,
  onApprove,
  onWriteTo,
}: {
  chatId: string;
  items: TranscriptItem[];
  status: ChatStatus;
  /** The message the assistant is writing right now, as far as it has got. */
  draft: string;
  /** With the design beside it: less room, so less space between things. */
  compact: boolean;
  /** Who is in the chat. */
  members: Member[];
  /** Answers the question the assistant is waiting on. */
  onAnswer: (answer: string) => void;
  /** Agrees to the design the assistant is waiting on. */
  onApprove: () => Promise<void>;
  /** Writes to one agent: the answer to its question. */
  onWriteTo: (handle: string, text: string) => void;
}) {
  const last = items[items.length - 1];
  // Between steps the assistant is deciding what to do next. While a step is
  // in progress, the step itself shows that; while it is writing, its words do.
  // Background tasks don't count: the assistant doesn't wait for them.
  const thinking =
    status === "running" &&
    !draft &&
    !(last?.type === "assistant" && last.steps.some((step) => !step.result));

  // What the assistant is writing or thinking joins its last message if that
  // was the last thing said.
  const lastAuthor = last ? authorOf(last) : null;
  const trailing = lastAuthor?.kind === "assistant";
  const now = new Date(useNow(30000)).toISOString();

  return (
    <RoomContext.Provider value={{ chatId, members, onWriteTo }}>
      <div className="flex flex-col">
        {items.map((item, index) => {
          const author = authorOf(item);
          const before = index > 0 ? items[index - 1] : undefined;
          const grouped =
            !!before &&
            sameAuthor(author, authorOf(before)) &&
            Date.parse(item.at) - Date.parse(before.at) < GROUP_MS;

          return (
            <div
              key={item.seq}
              className={cn(index > 0 && (grouped ? "mt-2" : compact ? "mt-5" : "mt-6"))}
            >
              <ItemView
                item={item}
                author={author}
                grouped={grouped}
                status={status}
                onAnswer={onAnswer}
                onApprove={onApprove}
              />
            </div>
          );
        })}

        {(draft || thinking) && (
          <div className={cn(items.length > 0 && (trailing ? "mt-2" : "mt-6"))}>
            <Row author={{ kind: "assistant" }} at={now} grouped={trailing}>
              {draft ? (
                // Shown as it is written, with a mark where the next word will
                // go; replaced by the finished message.
                <div aria-live="off" data-draft className="[&>div>p:last-child]:caret-after">
                  <Markdown>{draft}</Markdown>
                </div>
              ) : (
                <p className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
                  <Spinner />
                  Thinking…
                </p>
              )}
            </Row>
          </div>
        )}
      </div>
    </RoomContext.Provider>
  );
}

function ItemView({
  item,
  author,
  grouped,
  status,
  onAnswer,
  onApprove,
}: {
  item: TranscriptItem;
  author: Author | null;
  grouped: boolean;
  status: ChatStatus;
  onAnswer: (answer: string) => void;
  onApprove: () => Promise<void>;
}) {
  const { chatId, members } = useRoom();

  switch (item.type) {
    case "user": {
      const agents = item.to.filter((handle) => handle !== ASSISTANT);
      return (
        <Row
          author={author!}
          at={item.at}
          grouped={grouped}
          detail={agents.length > 0 ? `to ${agents.map((h) => "@" + h).join(", ")}` : undefined}
        >
          {item.text && (
            <p className="text-sm leading-relaxed wrap-anywhere whitespace-pre-wrap">
              <Mentions text={item.text} handles={members.map((m) => m.handle)} />
            </p>
          )}
          <AttachmentList chatId={chatId} files={item.attachments} />
          {item.answering.map((one) => (
            <AnsweringLine key={one.handle} answering={one} at={item.at} />
          ))}
        </Row>
      );
    }

    case "assistant":
      return (
        <Row author={author!} at={item.at} grouped={grouped}>
          {item.text && <Markdown>{item.text}</Markdown>}
          {item.steps.length > 0 && (
            <div className="flex flex-col gap-2">
              {item.steps.map((step) => (
                <StepView
                  key={step.call.id}
                  step={step}
                  outcome={outcomeOf(step, status)}
                  status={status}
                  onAnswer={onAnswer}
                  onApprove={onApprove}
                />
              ))}
            </div>
          )}
        </Row>
      );

    case "post":
      return <PostView item={item} grouped={grouped} />;

    case "news":
      return (
        <Aside>
          <NewsLine news={item.news} />
        </Aside>
      );

    case "exchange":
      return (
        <Aside>
          <ExchangeView exchange={item.exchange} />
        </Aside>
      );

    case "error":
      return (
        <Row author={author!} at={item.at} grouped={grouped}>
          {item.stopped ? (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <CircleSlashIcon className="size-4" />
              You stopped the assistant.
            </p>
          ) : (
            <div
              role="alert"
              className="flex items-start gap-2.5 rounded-lg border border-destructive/40 px-4 py-3 text-sm text-destructive"
            >
              <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
              <p className="min-w-0 wrap-anywhere">{item.message}</p>
            </div>
          )}
        </Row>
      );
  }
}

/**
 * One message: who wrote it and when, beside what they wrote. Following
 * another by the same author shortly after, it leaves both out, and shows the
 * time only on hover, in the picture's place.
 */
function Row({
  author,
  at,
  grouped,
  detail,
  children,
}: {
  author: Author;
  at: string;
  grouped: boolean;
  /** A few quiet words after the name: whom it was for, or what an agent does. */
  detail?: string;
  children: ReactNode;
}) {
  const { members } = useRoom();
  const shown: Author =
    author.kind === "agent" ? { ...author, member: findMember(members, author.handle) } : author;
  const name =
    author.kind === "you" ? "You" : author.kind === "assistant" ? "Assistant" : author.handle;

  return (
    <div className="group/row flex min-w-0 gap-3">
      <div className="flex w-8 shrink-0 justify-center">
        {grouped ? (
          <time
            dateTime={at}
            title={formatDateTime(at)}
            className="pt-0.5 text-[10px] text-muted-foreground opacity-0 tabular-nums transition-opacity group-hover/row:opacity-100"
          >
            {clock(at)}
          </time>
        ) : (
          <Avatar author={shown} />
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        {!grouped && (
          <p className="flex min-w-0 items-baseline gap-2 text-sm leading-5">
            <span className="shrink-0 font-semibold">{name}</span>
            {detail && (
              <span className="min-w-0 truncate text-xs text-muted-foreground">{detail}</span>
            )}
            <time
              dateTime={at}
              title={formatDateTime(at)}
              className="shrink-0 text-xs text-muted-foreground/80 tabular-nums"
            >
              {clock(at)}
            </time>
          </p>
        )}
        {children}
      </div>
    </div>
  );
}

/** Something the platform says between messages, in line with what people wrote. */
function Aside({ children }: { children: ReactNode }) {
  return <div className="pl-11">{children}</div>;
}

/** Text with the names of people in the chat picked out where they are mentioned. */
function Mentions({ text, handles }: { text: string; handles: string[] }) {
  const parts = text.split(/(@[a-z][a-z0-9-]*)/gi);

  return (
    <>
      {parts.map((part, index) =>
        part.startsWith("@") && handles.includes(part.slice(1).toLowerCase()) ? (
          <span key={index} className="rounded-sm bg-secondary px-1 font-medium">
            {part}
          </span>
        ) : (
          part
        ),
      )}
    </>
  );
}

/**
 * Under a message written to an agent: that it is working on it, and what
 * it is doing, until its answer arrives (which is shown where it arrives).
 */
function AnsweringLine({ answering, at }: { answering: Answering; at: string }) {
  const { members } = useRoom();
  const now = useNow(5000);
  if (answering.answered) return null;

  const member = findMember(members, answering.handle);
  const author: Author = { kind: "agent", handle: answering.handle, member };
  const doing = answering.activity[answering.activity.length - 1];
  const starting = now - Date.parse(at) < STARTING_MS;

  let line: ReactNode;
  if (answering.queued) {
    line = (
      <span className="text-muted-foreground">
        <span className="font-medium text-foreground">{answering.handle}</span> is busy with another
        task, and will read this when it&apos;s done.
      </span>
    );
  } else if (member?.status === "working" || starting) {
    line = (
      <span className="flex min-w-0 items-center gap-2 text-muted-foreground">
        <Spinner className="shrink-0 text-primary" />
        <span className="min-w-0 truncate">
          <span className="font-medium text-foreground">{answering.handle}</span> is on it
          {/* Each step it takes changes this; read out, it would be a running commentary. */}
          {doing && (
            <span aria-live="off" className="text-muted-foreground/80">
              : {doing}
            </span>
          )}
        </span>
      </span>
    );
  } else {
    line = (
      <span className="text-muted-foreground">
        <span className="font-medium text-foreground">{answering.handle}</span> stopped before
        answering.
      </span>
    );
  }

  return (
    <div role="status" className="flex items-center gap-2 text-sm">
      <Avatar author={author} size="sm" />
      {line}
    </div>
  );
}

/** Something an agent wrote: an answer, something to look at, or a question. */
function PostView({
  item,
  grouped,
}: {
  item: Extract<TranscriptItem, { type: "post" }>;
  grouped: boolean;
}) {
  const { chatId, onWriteTo } = useRoom();
  const { post, quote, answer } = item;
  const open = post.question && answer === undefined;

  const body = (
    <>
      {post.failed ? (
        <p
          className={cn(
            "flex items-start gap-2 text-sm wrap-anywhere",
            post.stopped ? "text-muted-foreground" : "text-destructive",
          )}
        >
          {post.stopped ? (
            <CircleSlashIcon className="mt-0.5 size-4 shrink-0" />
          ) : (
            <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
          )}
          {post.stopped
            ? "Stopped before answering."
            : `Couldn't answer: ${post.text.replace(/^Error: /, "")}`}
        </p>
      ) : (
        post.text && <Markdown>{post.text}</Markdown>
      )}
      <AttachmentList chatId={chatId} files={post.attachments ?? []} />
      {post.preview && <PostPreviewButton post={post} />}
    </>
  );

  return (
    <Row
      author={{ kind: "agent", handle: post.agent }}
      at={item.at}
      grouped={grouped}
      detail={roleText(post.role, post.machine.name)}
    >
      {quote !== undefined && (
        <p className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <CornerDownRightIcon className="size-3.5 shrink-0" />
          <span className="shrink-0">Answering you:</span>
          <span className="min-w-0 truncate italic">{quote || "your files"}</span>
        </p>
      )}

      {post.question ? (
        <div
          className={cn(
            "flex flex-col gap-3 rounded-xl border bg-card p-4",
            // Waiting on the user: outlined in the accent, the one thing to act on.
            open && "border-primary/60",
          )}
        >
          <div className="flex items-start gap-2.5">
            <MessageCircleQuestionIcon
              className={cn(
                "mt-0.5 size-4 shrink-0",
                open ? "text-primary" : "text-muted-foreground",
              )}
            />
            <div className="flex min-w-0 flex-1 flex-col gap-3">{body}</div>
          </div>

          {answer !== undefined ? (
            <p className="flex items-start gap-2.5 text-sm font-medium">
              <CheckIcon className="mt-0.5 size-4 shrink-0 text-success" />
              <span className="min-w-0 wrap-anywhere whitespace-pre-wrap">{answer}</span>
            </p>
          ) : (
            <>
              {(post.options?.length ?? 0) > 0 && (
                <div className="flex flex-col gap-2" role="group" aria-label="Answers">
                  {post.options!.map((option, index) => (
                    <button
                      key={option}
                      type="button"
                      onClick={() => onWriteTo(post.agent, option)}
                      className="flex items-center gap-2 rounded-lg bg-muted/60 px-3.5 py-2.5 text-left text-sm font-medium transition-colors outline-none hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50"
                    >
                      <span className="min-w-0 wrap-anywhere">{option}</span>
                      {/* Agents are told to put their own pick first. */}
                      {index === 0 && post.options!.length > 1 && (
                        <Badge variant="secondary" className="shrink-0">
                          Suggested
                        </Badge>
                      )}
                    </button>
                  ))}
                </div>
              )}
              <p className="text-xs text-muted-foreground">
                Or write to @{post.agent} below. Your answer goes to {post.agent} only.
              </p>
            </>
          )}
        </div>
      ) : (
        body
      )}
    </Row>
  );
}

/** A button opening a page an agent serves, as it asked. */
function PostPreviewButton({ post }: { post: AgentPost }) {
  const [opening, setOpening] = useState(false);
  const preview = post.preview!;
  const label = preview.label || `Port ${preview.port}${preview.path ?? ""}`;

  async function open() {
    // Opened now, while the browser still counts this as the user's click;
    // see Preview below.
    const tab = window.open("", "_blank");
    setOpening(true);
    try {
      const url = await portUrl(post.machine.id, preview.port, preview.path || "/");
      if (tab) {
        tab.opener = null;
        tab.location.href = url;
      } else {
        window.location.href = url;
      }
    } catch (err) {
      tab?.close();
      toast.error(`Couldn't open ${label}`, { description: errorMessage(err) });
    } finally {
      setOpening(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <Button variant="outline" size="sm" onClick={open} disabled={opening}>
        {opening ? (
          <Spinner data-icon="inline-start" />
        ) : (
          <ExternalLinkIcon data-icon="inline-start" />
        )}
        Open {label}
      </Button>
      <span className="font-mono text-xs text-muted-foreground">
        {post.machine.name}:{preview.port}
        {preview.path}
      </span>
    </div>
  );
}

function StepView({
  step,
  outcome,
  status,
  onAnswer,
  onApprove,
}: {
  step: Step;
  outcome: Outcome;
  status: ChatStatus;
  onAnswer: (answer: string) => void;
  onApprove: () => Promise<void>;
}) {
  const machine = text(step.args.machine) || text(step.args.name);
  // The newest note says what a step in progress is doing right now.
  const doing = outcome === "working" ? step.activity[step.activity.length - 1] : undefined;

  switch (step.name) {
    case "ask_user":
      return (
        <Question step={step} open={!step.result && status === "waiting"} onAnswer={onAnswer} />
      );

    case "propose_architecture":
      return (
        <Proposal step={step} open={!step.result && status === "waiting"} onApprove={onApprove} />
      );

    case "update_architecture":
      return (
        <StepLine outcome={outcome} error={step.result?.content}>
          {outcome === "working" ? "Updating the design" : "Updated the design"}
        </StepLine>
      );

    case "code_on_machine":
      return <CodingTask step={step} outcome={outcome} machine={machine} />;

    case "show_preview":
      return <Preview step={step} outcome={outcome} machine={machine} />;

    case "list_machines":
      return (
        <StepLine outcome={outcome} error={step.result?.content}>
          {outcome === "working" ? "Looking at your machines" : "Looked at your machines"}
        </StepLine>
      );

    case "list_agents":
      return (
        <StepLine outcome={outcome} error={step.result?.content}>
          {outcome === "working" ? "Checking on the agents" : "Checked on the agents"}
        </StepLine>
      );

    case "stop_agent":
      return (
        <StepLine outcome={outcome} error={step.result?.content}>
          {outcome === "working" ? "Stopping " : "Stopped "}
          <span className="font-medium text-foreground">{agentOf(step)}</span>
          {" on "}
          <MachineName step={step} name={machine} />
        </StepLine>
      );

    case "create_machine": {
      const made = step.result?.machine;

      return (
        <StepLine
          outcome={outcome}
          error={step.result?.content}
          detail={
            doing ??
            (made &&
              `${made.cpus} vCPU · ${formatMemory(made.memory_mb)} · ${made.disk_gb} GB disk`)
          }
        >
          {outcome === "working" ? "Creating machine " : "Created machine "}
          <MachineName step={step} name={machine} />
        </StepLine>
      );
    }

    case "start_machine":
      return (
        <StepLine outcome={outcome} error={step.result?.content} detail={doing}>
          {outcome === "working" ? "Starting " : "Started "}
          <MachineName step={step} name={machine} />
        </StepLine>
      );

    case "stop_machine":
      return (
        <StepLine outcome={outcome} error={step.result?.content} detail={doing}>
          {outcome === "working" ? "Stopping " : "Stopped "}
          <MachineName step={step} name={machine} />
        </StepLine>
      );

    default:
      return (
        <StepLine outcome={outcome} error={step.result?.content}>
          {step.name}
        </StepLine>
      );
  }
}

function OutcomeIcon({ outcome }: { outcome: Outcome }) {
  switch (outcome) {
    case "working":
      return <Spinner className="shrink-0 text-primary" />;
    case "done":
      return <CheckIcon className="size-4 shrink-0 text-success" />;
    case "failed":
      return <XIcon className="size-4 shrink-0 text-destructive" />;
    case "interrupted":
      return <CircleSlashIcon className="size-4 shrink-0 text-muted-foreground" />;
  }
}

/** A step that is one line: what it is, and how it went. */
function StepLine({
  outcome,
  error,
  detail,
  children,
}: {
  outcome: Outcome;
  /** The step's result, shown only if it failed. */
  error?: string;
  /** A few quieter words after the line: what it is doing, or what came of it. */
  detail?: string;
  children: ReactNode;
}) {
  return (
    <div className="text-sm">
      <p className="flex items-center gap-2 text-muted-foreground">
        <OutcomeIcon outcome={outcome} />
        <span className="min-w-0 truncate">
          {children}
          {outcome === "interrupted" && " (did not finish)"}
          {detail && outcome !== "failed" && outcome !== "interrupted" && (
            <span className="text-muted-foreground/70"> · {detail}</span>
          )}
        </span>
      </p>
      {outcome === "failed" && error && (
        <p className="mt-1 pl-6 wrap-anywhere text-destructive">{error.replace(/^Error: /, "")}</p>
      )}
    </div>
  );
}

/** A machine's name, linking to the machine once the step says which it is. */
function MachineName({ step, name }: { step: Step; name: string }) {
  const machine = step.result?.machine;
  const label = <span className="font-mono text-[13px] text-foreground">{name || "machine"}</span>;

  if (!machine) return label;

  return (
    <Link href={`/instances/${machine.id}`} className="underline-offset-4 hover:underline">
      {label}
    </Link>
  );
}

type Option = { label: string; description?: string };

/** The answers a question offers. Older chats stored them as plain strings. */
function optionsOf(value: unknown): Option[] {
  if (!Array.isArray(value)) return [];

  return value.flatMap((option): Option[] => {
    if (typeof option === "string") return [{ label: option }];
    if (option && typeof option === "object" && typeof option.label === "string") {
      return [{ label: option.label, description: text(option.description) || undefined }];
    }
    return [];
  });
}

function Question({
  step,
  open,
  onAnswer,
}: {
  step: Step;
  /** The assistant is waiting for this to be answered. */
  open: boolean;
  onAnswer: (answer: string) => void;
}) {
  const options = optionsOf(step.args.options);
  const question = text(step.args.question);
  const answer = step.result?.content;

  // Answered: what was asked and what was chosen, in two lines. The options
  // not taken are of no further interest.
  if (answer !== undefined) {
    return (
      <div className="rounded-lg border bg-card px-4 py-3 text-sm">
        <p className="flex items-start gap-2.5 text-muted-foreground">
          <MessageCircleQuestionIcon className="mt-0.5 size-4 shrink-0" />
          <span className="min-w-0 wrap-anywhere">{question}</span>
        </p>
        <p className="mt-1.5 flex items-start gap-2.5 font-medium">
          <CheckIcon className="mt-0.5 size-4 shrink-0 text-success" />
          <span className="min-w-0 wrap-anywhere whitespace-pre-wrap">{answer}</span>
        </p>
      </div>
    );
  }

  return (
    <div
      className={cn(
        "rounded-xl border bg-card p-4",
        // Waiting on the user: outlined in the accent, the one thing to act on.
        open && "border-primary/60",
      )}
    >
      <p className="flex items-start gap-2.5 text-sm font-medium">
        <MessageCircleQuestionIcon className="mt-0.5 size-4 shrink-0 text-primary" />
        <span className="min-w-0 wrap-anywhere">{question}</span>
      </p>

      {options.length > 0 && (
        <div className="mt-3 flex flex-col gap-2" role="group" aria-label="Answers">
          {options.map((option, index) => (
            <button
              key={option.label}
              type="button"
              disabled={!open}
              onClick={() => onAnswer(option.label)}
              className="group/option rounded-lg bg-muted/60 px-3.5 py-2.5 text-left text-sm transition-colors outline-none hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-60"
            >
              <span className="flex items-center gap-2">
                <span className="min-w-0 font-medium wrap-anywhere">{option.label}</span>
                {/* The assistant is told to put its own pick first. */}
                {index === 0 && options.length > 1 && (
                  <Badge variant="secondary" className="shrink-0">
                    Suggested
                  </Badge>
                )}
              </span>
              {option.description && (
                <span className="mt-0.5 block wrap-anywhere text-muted-foreground">
                  {option.description}
                </span>
              )}
            </button>
          ))}
        </div>
      )}

      {open ? (
        <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">
            {options.length > 0 ? "Or type your own answer below." : "Type your answer below."}
          </p>
          <Button variant="ghost" size="xs" onClick={() => onAnswer("You decide.")}>
            You decide
          </Button>
        </div>
      ) : (
        <p className="mt-3 text-xs text-muted-foreground">This question was withdrawn.</p>
      )}
    </div>
  );
}

/** A coding task given to the agent inside a machine, with what it is doing. */
function CodingTask({ step, outcome, machine }: { step: Step; outcome: Outcome; machine: string }) {
  const log = useRef<HTMLOListElement>(null);
  const lines = step.activity;

  // Follow the newest line while the agent works.
  useEffect(() => {
    if (outcome === "working") log.current?.scrollTo({ top: log.current.scrollHeight });
  }, [lines.length, outcome]);

  const doing = {
    working: " is coding on ",
    done: " finished on ",
    failed: " failed on ",
    interrupted: " was stopped on ",
  }[outcome];

  const background = isBackground(step);
  const { members } = useRoom();
  const member = memberFor(members, agentOf(step), machine);
  // A background task's report comes with the news that it finished; the
  // call's own result only said that it had started.
  const outcomeText = background ? step.news?.content : step.result?.content;

  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <div className="flex items-center gap-2.5 px-4 py-3 text-sm">
        {member ? (
          <Avatar author={{ kind: "agent", handle: member.handle, member }} size="sm" />
        ) : (
          <SquareTerminalIcon className="size-4 shrink-0 text-muted-foreground" />
        )}
        <p className="min-w-0 flex-1 truncate">
          <span className="font-medium">{member ? member.handle : agentOf(step)}</span>
          {member && <span className="text-muted-foreground"> ({member.role})</span>}
          <span className="text-muted-foreground">{doing}</span>
          <MachineName step={step} name={machine} />
        </p>
        {background && (
          <Badge variant="outline" className="shrink-0 text-muted-foreground">
            Background
          </Badge>
        )}
        <OutcomeIcon outcome={outcome} />
      </div>

      <Disclosure label="Task" className="border-t">
        <p className="text-sm wrap-anywhere whitespace-pre-wrap text-muted-foreground">
          {text(step.args.task)}
        </p>
      </Disclosure>

      {lines.length > 0 && (
        <ol
          ref={log}
          aria-label={`What the coding agent did on ${machine}`}
          className="max-h-44 overflow-y-auto overscroll-contain bg-black/25 px-4 py-2.5 font-mono text-xs leading-relaxed"
          translate="no"
        >
          {lines.map((line, index) => (
            <LogLine
              key={index}
              line={line}
              current={outcome === "working" && index === lines.length - 1}
            />
          ))}
        </ol>
      )}

      {outcomeText && outcome !== "working" && outcome !== "interrupted" && (
        <Disclosure
          label={outcome === "failed" ? "What went wrong" : "Report"}
          className="border-t"
          defaultOpen={outcome === "failed"}
        >
          {outcome === "failed" ? (
            <p className="text-sm wrap-anywhere text-destructive">
              {outcomeText.replace(/^Error: /, "")}
            </p>
          ) : (
            <Markdown className="text-muted-foreground">{outcomeText}</Markdown>
          )}
        </Disclosure>
      )}
    </div>
  );
}

/**
 * How each kind of line in an agent's log begins, as the server writes it,
 * and the word it is filed under here. A command the agent ran is the thing
 * most worth finding again, so those are the bright ones.
 */
const LOG_KINDS: { begins: string; kind: string; bright?: boolean }[] = [
  { begins: "Running ", kind: "run", bright: true },
  { begins: "Writing ", kind: "write" },
  { begins: "Editing ", kind: "edit" },
  { begins: "Reading ", kind: "read" },
  { begins: "Searching for ", kind: "search" },
  { begins: "Listing ", kind: "list" },
  { begins: "Fetching ", kind: "fetch" },
  { begins: "Registering service ", kind: "service", bright: true },
  { begins: "Asking ", kind: "ask" },
];

/**
 * One thing an agent did, as a line of a log: what kind of thing in a
 * column of its own, so the eye can run down the commands or the files.
 */
function LogLine({ line, current }: { line: string; current: boolean }) {
  const kind = LOG_KINDS.find((k) => line.startsWith(k.begins));
  const rest = kind ? line.slice(kind.begins.length) : line;

  return (
    <li className="flex gap-2" title={line}>
      <span aria-hidden className="w-[7ch] shrink-0 text-muted-foreground/60 select-none">
        {kind?.kind ?? "·"}
      </span>
      <span
        className={cn(
          "min-w-0 truncate",
          current || kind?.bright ? "text-foreground" : "text-muted-foreground",
          current && "caret-after",
        )}
      >
        {/* Read aloud as it was written, not as the column heading. */}
        {kind && <span className="sr-only">{kind.begins}</span>}
        {rest}
      </span>
    </li>
  );
}

/**
 * A design the assistant has put to the user: what it is made of, and a way
 * to agree to it. The diagram beside the chat shows the same design, and is
 * where it can be changed.
 */
function Proposal({
  step,
  open,
  onApprove,
}: {
  step: Step;
  /** The assistant is waiting for this to be agreed to. */
  open: boolean;
  onApprove: () => Promise<void>;
}) {
  const [approving, setApproving] = useState(false);
  const { designOpen, setDesignOpen, paneTab, setPaneTab } = useAssistant();

  const nodes = Array.isArray(step.args.nodes) ? (step.args.nodes as DesignNode[]) : [];
  const reasons = Array.isArray(step.args.reasons)
    ? step.args.reasons.filter((reason): reason is string => typeof reason === "string")
    : [];
  const result = step.result;

  // A design that didn't hold together never reached the user.
  if (result?.failed) {
    return (
      <StepLine outcome="failed" error={result.content}>
        Proposed a design
      </StepLine>
    );
  }

  const approved = result?.content.startsWith("The user approved");
  // What the user said in place of approving, as the assistant was told it.
  const asked =
    result && !approved ? result.content.match(/They said:\n\n([\s\S]*?)\n\nIf they/)?.[1] : null;

  async function approve() {
    setApproving(true);
    try {
      await onApprove();
    } finally {
      setApproving(false);
    }
  }

  return (
    <div
      className={cn(
        "rounded-xl border bg-card p-4",
        // Waiting on the user: outlined in the accent, the one thing to act on.
        open && "border-primary/60",
      )}
    >
      <p className="flex items-start gap-2.5 text-sm font-medium">
        <NetworkIcon className="mt-0.5 size-4 shrink-0 text-primary" />
        <span className="min-w-0 wrap-anywhere">
          {approved ? "Design approved" : asked ? "Design, before your changes" : "Proposed design"}
        </span>
        {approved && <CheckIcon className="mt-0.5 ml-auto size-4 shrink-0 text-success" />}
      </p>
      <p className="mt-2 text-sm wrap-anywhere text-muted-foreground">{text(step.args.summary)}</p>

      <ul className="mt-3 flex flex-col gap-1.5 text-sm">
        {nodes.map((node) => (
          <li key={node.id} className="flex items-baseline gap-2">
            <span className="min-w-0 truncate font-medium">{node.name}</span>
            <span className="min-w-0 truncate text-muted-foreground">{node.tech}</span>
            {node.machine && (
              <span className="ml-auto shrink-0 font-mono text-xs text-muted-foreground">
                {node.machine}
              </span>
            )}
          </li>
        ))}
      </ul>

      {/* The case for it, open while it is being decided on. */}
      {reasons.length > 0 && (
        <details className="group mt-3 border-t pt-3" open={open}>
          <summary className="flex cursor-pointer list-none items-center gap-1.5 rounded-sm text-sm font-medium outline-none select-none focus-visible:ring-[3px] focus-visible:ring-ring/50 [&::-webkit-details-marker]:hidden">
            <ChevronRightIcon className="size-4 transition-transform group-open:rotate-90" />
            Why this design
          </summary>
          <ul className="mt-2 list-disc space-y-1.5 pl-6 text-sm wrap-anywhere text-muted-foreground marker:text-muted-foreground/60">
            {reasons.map((reason) => (
              <li key={reason}>{reason}</li>
            ))}
          </ul>
        </details>
      )}

      {open && (
        <div className="mt-4 flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">
            The diagram shows how it fits together. Say below what to change.
          </p>
          <div className="flex items-center gap-2">
            {(!designOpen || paneTab !== "graph") && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setPaneTab("graph");
                  setDesignOpen(true);
                }}
              >
                Show the diagram
              </Button>
            )}
            <Button size="sm" onClick={approve} disabled={approving}>
              {approving ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <CheckIcon data-icon="inline-start" />
              )}
              Approve and build
            </Button>
          </div>
        </div>
      )}
      {asked && (
        <p className="mt-3 border-t pt-3 text-sm">
          <span className="text-muted-foreground">You asked for a change: </span>
          <span className="wrap-anywhere whitespace-pre-wrap">{asked}</span>
        </p>
      )}
      {!open && !result && (
        <p className="mt-3 text-xs text-muted-foreground">This proposal was withdrawn.</p>
      )}
    </div>
  );
}

/** One agent asking another something, and what it was told. */
function ExchangeView({ exchange }: { exchange: AgentExchange }) {
  return (
    <details className="group rounded-lg border bg-card">
      <summary className="flex cursor-pointer list-none items-center gap-2 rounded-lg px-4 py-2.5 text-sm outline-none select-none focus-visible:ring-[3px] focus-visible:ring-ring/50 [&::-webkit-details-marker]:hidden">
        <MessagesSquareIcon className="size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate">
          <span className="font-medium">{exchange.from.agent}</span>
          <span className="text-muted-foreground">
            {" "}
            on <span className="font-mono text-xs">{exchange.from.machine}</span>{" "}
            {exchange.queued ? "left a question for" : "asked"}{" "}
          </span>
          <span className="font-medium">{exchange.to.agent}</span>
          <span className="text-muted-foreground">
            {" "}
            on <span className="font-mono text-xs">{exchange.to.machine}</span>
          </span>
        </span>
        <ChevronRightIcon className="size-3.5 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
      </summary>
      <div className="flex flex-col gap-2 border-t px-4 py-3 text-sm">
        <p className="wrap-anywhere whitespace-pre-wrap">{exchange.question}</p>
        {exchange.queued ? (
          <p className="text-xs text-muted-foreground">
            {exchange.to.agent} was busy. It gets the question with its next task.
          </p>
        ) : (
          <div
            className={cn(
              "border-l-2 pl-3",
              exchange.failed ? "border-destructive text-destructive" : "text-muted-foreground",
            )}
          >
            <Markdown>{exchange.answer ?? ""}</Markdown>
          </div>
        )}
      </div>
    </details>
  );
}

/** Where in the conversation a background agent finished. */
function NewsLine({ news }: { news: AgentNews }) {
  const how = news.stopped ? "was stopped" : news.failed ? "failed" : "finished";

  return (
    <p role="status" className="flex items-center gap-2 text-xs text-muted-foreground">
      <span
        aria-hidden
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          news.stopped ? "bg-muted-foreground" : news.failed ? "bg-destructive" : "bg-success",
        )}
      />
      <span className="min-w-0 truncate">
        <span className="font-medium text-foreground">{news.handle ?? news.agent}</span>
        {news.handle && ` (${news.agent})`} on{" "}
        <span className="font-mono">{news.machine.name}</span> {how}
      </span>
      <span aria-hidden className="h-px min-w-6 flex-1 bg-border" />
    </p>
  );
}

/** A section that opens and closes, for detail most people won't need. */
function Disclosure({
  label,
  defaultOpen = false,
  className,
  children,
}: {
  label: string;
  defaultOpen?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <details className={cn("group", className)} open={defaultOpen}>
      <summary className="flex cursor-pointer list-none items-center gap-1.5 px-4 py-2 text-xs font-medium text-muted-foreground outline-none select-none hover:text-foreground focus-visible:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:ring-inset [&::-webkit-details-marker]:hidden">
        <ChevronRightIcon className="size-3.5 transition-transform group-open:rotate-90" />
        {label}
      </summary>
      <div className="px-4 pb-3 pl-9.5">{children}</div>
    </details>
  );
}

/** A button that opens something a machine is serving. */
function Preview({ step, outcome, machine }: { step: Step; outcome: Outcome; machine: string }) {
  const [opening, setOpening] = useState(false);

  const port = typeof step.args.port === "number" ? step.args.port : 0;
  const label = text(step.args.label) || `Port ${port}`;
  const target = step.result?.machine;

  if (outcome === "failed" || outcome === "interrupted") {
    return (
      <StepLine outcome={outcome} error={step.result?.content}>
        Preview of {label}
      </StepLine>
    );
  }

  async function open() {
    if (!target) return;

    // Opened now, while the browser still counts this as the user's click,
    // and pointed at the page once its link arrives: a window opened after
    // waiting would be blocked as a pop-up.
    const tab = window.open("", "_blank");
    setOpening(true);
    try {
      const url = await portUrl(target.id, port);
      if (tab) {
        tab.opener = null;
        tab.location.href = url;
      } else {
        window.location.href = url;
      }
    } catch (err) {
      tab?.close();
      toast.error(`Couldn't open ${label}`, { description: errorMessage(err) });
    } finally {
      setOpening(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <Button variant="outline" size="sm" onClick={open} disabled={!target || opening}>
        {opening ? (
          <Spinner data-icon="inline-start" />
        ) : (
          <ExternalLinkIcon data-icon="inline-start" />
        )}
        Open {label}
      </Button>
      <span className="font-mono text-xs text-muted-foreground">
        {machine}:{port}
      </span>
    </div>
  );
}
