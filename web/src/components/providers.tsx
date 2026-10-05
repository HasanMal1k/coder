"use client";

import type { ReactNode } from "react";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";

/** What every page needs around it: tooltips and notifications. */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <TooltipProvider delayDuration={300}>
      {children}
      <Toaster position="bottom-right" />
    </TooltipProvider>
  );
}
