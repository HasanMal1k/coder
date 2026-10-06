"use client";

import { ArrowUpIcon, SquareIcon } from "lucide-react";
import { useEffect, useRef, type KeyboardEvent, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Button } from "../ui/button";
import { Kbd } from "../ui/kbd";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";

/**
 * Where the user types. While the assistant is working the send button
 * becomes a stop button; what has been typed is kept for when it finishes.
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
}) {
  const input = useRef<HTMLTextAreaElement>(null);

  // Ready to type as soon as the page opens, and again once the assistant
  // has finished. Only where there is a keyboard to type on: on a phone,
  // focus brings the on-screen one up over the page the user came to read.
  useEffect(() => {
    if (working || disabled) return;
    if (window.matchMedia("(hover: hover) and (pointer: fine)").matches) input.current?.focus();
  }, [working, disabled]);

  const canSend = !working && !disabled && value.trim() !== "";

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    // Enter sends; Shift+Enter is a new line. Not while an input method is
    // composing a character, where Enter means "accept".
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      if (canSend) onSend();
    }
  }

  // Said under what is typed, when there is something to say.
  const line = working ? "Working. You can leave this page; it carries on." : note;

  return (
    <div
      className={cn(
        "relative rounded-xl border bg-card shadow-lg shadow-black/20 transition-colors focus-within:border-foreground/25",
        !disabled && "hover:border-foreground/20 focus-within:hover:border-foreground/25",
      )}
    >
      <textarea
        ref={input}
        name="message"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={onKeyDown}
        disabled={disabled}
        rows={1}
        placeholder={placeholder}
        aria-label="Message the assistant"
        autoComplete="off"
        className={cn(
          "field-sizing-content block max-h-60 w-full resize-none bg-transparent px-4 py-3.5 text-base leading-relaxed outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-60 md:text-sm",
          roomy ? "min-h-24" : "min-h-12",
          // Without a line beneath, the last line of text shares its row
          // with the button, and must stop short of it.
          line ? "pb-1" : "pr-14",
        )}
      />
      {line && <p className="truncate pr-14 pb-3.5 pl-4 text-xs text-muted-foreground">{line}</p>}

      {/* In the corner whatever the height, so it never moves as the text grows. */}
      <div className="absolute right-2 bottom-2">
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
