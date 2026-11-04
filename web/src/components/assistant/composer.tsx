"use client";

import { ArrowUpIcon, PaperclipIcon, SquareIcon } from "lucide-react";
import {
  useEffect,
  useId,
  useImperativeHandle,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
} from "react";
import { cn } from "@/lib/utils";
import { Button } from "../ui/button";
import { Kbd } from "../ui/kbd";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { PendingFiles, type PendingFile } from "./attachments";
import { Avatar, roleText, type Member } from "./people";

/** What a parent can do to the box: put the cursor in it, or start a mention. */
export type ComposerHandle = { focus: () => void; mention: (handle: string) => void };

/** The "@na" being typed just before the cursor, if any. */
const MENTIONING = /(^|[^\w@.])@([a-z0-9-]*)$/i;

/**
 * Where the user types. While the assistant is working the send button
 * becomes a stop button; what has been typed is kept for when it finishes.
 * Typing @ offers the chat's people; files can be attached, pasted or
 * dropped on it.
 */
export function Composer({
  value,
  onChange,
  onSend,
  onStop,
  working,
  disabled,
  placeholder,
  note,
  roomy = false,
  members = [],
  files,
  onAddFiles,
  onRemoveFile,
  sendBlocked = false,
  ref,
}: {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  onStop: () => void;
  /** The assistant is busy: offer to stop it instead of sending. */
  working: boolean;
  /** Nothing can be sent at all. */
  disabled: boolean;
  placeholder: string;
  /** A quiet line under what is typed, beside the send button. */
  note?: ReactNode;
  /** Taller, for the page where it is the only thing to do. */
  roomy?: boolean;
  /** Who can be mentioned with an @. */
  members?: Member[];
  /** Files chosen to go with the message. Without onAddFiles, none can be. */
  files?: PendingFile[];
  onAddFiles?: (files: File[]) => void;
  onRemoveFile?: (key: string) => void;
  /** Something is not ready to send (a file still uploading). */
  sendBlocked?: boolean;
  ref?: Ref<ComposerHandle>;
}) {
  const input = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const listId = useId();
  const [dragging, setDragging] = useState(false);
  // The mention being typed, and which of its matches is highlighted.
  const [query, setQuery] = useState<string | null>(null);
  const [highlight, setHighlight] = useState(0);

  // Ready to type as soon as the page opens, and again once the assistant
  // has finished. Only where there is a keyboard to type on: on a phone,
  // focus brings the on-screen one up over the page the user came to read.
  useEffect(() => {
    if (working || disabled) return;
    if (window.matchMedia("(hover: hover) and (pointer: fine)").matches) input.current?.focus();
  }, [working, disabled]);

  useImperativeHandle(ref, () => ({
    focus: () => input.current?.focus(),
    mention: (handle) => {
      const tag = `@${handle} `;
      if (!value.includes(tag)) onChange(value.trim() === "" ? tag : `${value.trimEnd()} ${tag}`);
      setQuery(null);
      requestAnimationFrame(() => {
        const el = input.current;
        if (!el) return;
        el.focus();
        el.setSelectionRange(el.value.length, el.value.length);
      });
    },
  }));

  const attachments = files ?? [];
  const hasContent = value.trim() !== "" || attachments.some((f) => f.state === "ready");
  const canSend = !working && !disabled && !sendBlocked && hasContent;

  const matches =
    query === null
      ? []
      : members.filter((m) => m.handle.startsWith(query.toLowerCase())).slice(0, 8);
  const open = matches.length > 0;
  const active = open ? matches[Math.min(highlight, matches.length - 1)] : undefined;

  // Works out, from where the cursor is, whether a mention is being typed.
  function look(el: HTMLTextAreaElement) {
    const before = el.value.slice(0, el.selectionStart);
    const found = MENTIONING.exec(before);
    setQuery(found ? found[2] : null);
    setHighlight(0);
  }

  function choose(member: Member) {
    const el = input.current;
    if (!el) return;
    const caret = el.selectionStart;
    const before = value.slice(0, caret).replace(/@[a-z0-9-]*$/i, `@${member.handle} `);
    const after = value.slice(caret).replace(/^[a-z0-9-]*\s?/i, "");
    onChange(before + after);
    setQuery(null);
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(before.length, before.length);
    });
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (open && active) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        const step = event.key === "ArrowDown" ? 1 : -1;
        setHighlight((matches.indexOf(active) + step + matches.length) % matches.length);
        return;
      }
      if ((event.key === "Enter" || event.key === "Tab") && !event.shiftKey) {
        event.preventDefault();
        choose(active);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        setQuery(null);
        return;
      }
    }

    // Enter sends; Shift+Enter is a new line. Not while an input method is
    // composing a character, where Enter means "accept".
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      if (canSend) onSend();
    }
  }

  function onPaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    if (!onAddFiles) return;
    const pasted = Array.from(event.clipboardData.files);
    if (pasted.length === 0) return;
    // A screenshot pasted is a file to send, not text.
    event.preventDefault();
    onAddFiles(pasted);
  }

  function onDragOver(event: DragEvent<HTMLDivElement>) {
    if (!onAddFiles || disabled || !event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
    setDragging(true);
  }

  function onDrop(event: DragEvent<HTMLDivElement>) {
    if (!onAddFiles || disabled) return;
    event.preventDefault();
    setDragging(false);
    onAddFiles(Array.from(event.dataTransfer.files));
  }

  // Said under what is typed, when there is something to say.
  const line = working ? "Working. You can leave this page; it carries on." : note;

  return (
    <div
      onDragOver={onDragOver}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragging(false);
      }}
      onDrop={onDrop}
      className={cn(
        "relative rounded-xl border bg-card shadow-lg shadow-black/20 transition-colors focus-within:border-foreground/25",
        !disabled && "hover:border-foreground/20 focus-within:hover:border-foreground/25",
        dragging && "border-primary/70 focus-within:border-primary/70 hover:border-primary/70",
      )}
    >
      {open && (
        <ul
          id={listId}
          role="listbox"
          aria-label="People to mention"
          className="absolute inset-x-0 bottom-full mb-2 overflow-hidden rounded-lg border bg-popover p-1 shadow-lg shadow-black/30"
        >
          {matches.map((member) => (
            <li
              key={member.handle}
              id={`${listId}-${member.handle}`}
              role="option"
              aria-selected={member === active}
              // Chosen on press, before the box loses focus.
              onMouseDown={(event) => {
                event.preventDefault();
                choose(member);
              }}
              onMouseEnter={() => setHighlight(matches.indexOf(member))}
              className={cn(
                "flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-sm",
                member === active && "bg-accent",
              )}
            >
              <Avatar
                size="sm"
                author={
                  member.kind === "assistant"
                    ? { kind: "assistant" }
                    : { kind: "agent", handle: member.handle, member }
                }
              />
              <span className="font-medium">@{member.handle}</span>
              <span className="min-w-0 truncate text-muted-foreground">
                {member.kind === "assistant"
                  ? "runs the project"
                  : roleText(member.role, member.machine?.name)}
              </span>
            </li>
          ))}
        </ul>
      )}

      {onRemoveFile && <PendingFiles files={attachments} onRemove={onRemoveFile} />}

      <textarea
        ref={input}
        name="message"
        value={value}
        onChange={(event) => {
          onChange(event.target.value);
          look(event.target);
        }}
        onKeyDown={onKeyDown}
        onKeyUp={(event) => {
          if (event.key === "ArrowLeft" || event.key === "ArrowRight") look(event.currentTarget);
        }}
        onClick={(event) => look(event.currentTarget)}
        onBlur={() => setQuery(null)}
        onPaste={onPaste}
        disabled={disabled}
        rows={1}
        placeholder={placeholder}
        aria-label="Message"
        autoComplete="off"
        role={members.length > 0 ? "combobox" : undefined}
        aria-autocomplete={members.length > 0 ? "list" : undefined}
        aria-expanded={members.length > 0 ? open : undefined}
        aria-controls={open ? listId : undefined}
        aria-activedescendant={active ? `${listId}-${active.handle}` : undefined}
        className={cn(
          "field-sizing-content block max-h-60 w-full resize-none bg-transparent px-4 py-3.5 text-base leading-relaxed outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-60 md:text-sm",
          roomy ? "min-h-24" : "min-h-12",
          // The buttons share the last row: with a line beneath, that row
          // is the line's; without, the text must stop short of them.
          line ? "pb-1" : onAddFiles ? "pr-24" : "pr-14",
        )}
      />
      {line && (
        <p
          className={cn(
            "truncate pb-3.5 pl-4 text-xs text-muted-foreground",
            onAddFiles ? "pr-24" : "pr-14",
          )}
        >
          {line}
        </p>
      )}

      {/* In the corner whatever the height, so they never move as the text grows. */}
      <div className="absolute right-2 bottom-2 flex items-center gap-1">
        {onAddFiles && (
          <>
            <input
              ref={picker}
              type="file"
              multiple
              hidden
              onChange={(event) => {
                onAddFiles(Array.from(event.target.files ?? []));
                event.target.value = "";
              }}
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label="Attach files"
                  disabled={disabled}
                  onClick={() => picker.current?.click()}
                  className="text-muted-foreground"
                >
                  <PaperclipIcon />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Attach files, or paste or drop them here</TooltipContent>
            </Tooltip>
          </>
        )}

        {working ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button size="icon-sm" variant="outline" aria-label="Stop" onClick={onStop}>
                <SquareIcon className="size-3! fill-current" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Stop</TooltipContent>
          </Tooltip>
        ) : (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                size="icon-sm"
                aria-label="Send"
                onClick={onSend}
                disabled={!canSend}
                // With nothing to send it is an outline, not a dimmed white block.
                className="disabled:border-border disabled:bg-transparent disabled:text-muted-foreground disabled:opacity-100"
              >
                <ArrowUpIcon />
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              Send <Kbd>Enter</Kbd>
            </TooltipContent>
          </Tooltip>
        )}
      </div>
    </div>
  );
}
