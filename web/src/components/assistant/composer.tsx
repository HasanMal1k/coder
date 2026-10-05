"use client";

import { ArrowUpIcon, SquareIcon } from "lucide-react";
import { useEffect, useRef, type KeyboardEvent } from "react";
import { Button } from "../ui/button";
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
}) {
  const input = useRef<HTMLTextAreaElement>(null);

  // Ready to type as soon as the page opens, and again once the assistant
  // has finished.
  useEffect(() => {
    if (!working && !disabled) input.current?.focus();
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

  return (
    <div className="rounded-md border bg-card p-2 transition-colors focus-within:border-foreground/50">
      <textarea
        ref={input}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={onKeyDown}
        disabled={disabled}
        rows={1}
        placeholder={placeholder}
        aria-label="Message the assistant"
        className="field-sizing-content block max-h-48 min-h-10 w-full resize-none bg-transparent px-3 py-2 text-base outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-60 md:text-sm"
      />
      <div className="flex items-center justify-between gap-2 pr-1 pl-3">
        <p className="truncate text-xs text-muted-foreground">
          {working
            ? "Working. You can leave this page; it carries on."
            : "Enter to send, Shift+Enter for a new line"}
        </p>
        {working ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button size="icon-sm" variant="secondary" aria-label="Stop" onClick={onStop}>
                <SquareIcon className="fill-current" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Stop</TooltipContent>
          </Tooltip>
        ) : (
          <Button size="icon-sm" aria-label="Send" onClick={onSend} disabled={!canSend}>
            <ArrowUpIcon />
          </Button>
        )}
      </div>
    </div>
  );
}
