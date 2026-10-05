"use client";

import { CheckIcon, CopyIcon } from "lucide-react";
import { useState } from "react";
import { Button } from "./ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";

/** Copies `value` to the clipboard and confirms by turning into a tick briefly. */
export function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access denied; nothing useful to do.
    }
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={copied ? "Copied" : label}
          onClick={copy}
        >
          {copied ? <CheckIcon className="text-success" /> : <CopyIcon />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{copied ? "Copied" : label}</TooltipContent>
    </Tooltip>
  );
}
