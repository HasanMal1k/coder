"use client";

import { SearchIcon, WifiOffIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useInstances } from "@/lib/instances";
import { usePaletteShortcut } from "@/lib/use-platform";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Kbd } from "../ui/kbd";
import { SidebarTrigger } from "../ui/sidebar";
import { useShell } from "./shell-provider";

/**
 * The bar across the top of every signed-in page: the sidebar toggle, where
 * you are, anything the page wants to add, then search.
 */
export function PageHeader({
  children,
  actions,
}: {
  /** Where you are: a breadcrumb or a title. */
  children: ReactNode;
  /** Page-specific controls, shown before search. */
  actions?: ReactNode;
}) {
  const { openPalette } = useShell();
  const { loadError } = useInstances();
  const shortcut = usePaletteShortcut();

  return (
    <header className="flex h-14 shrink-0 items-center gap-2 border-b px-3 sm:px-4">
      <SidebarTrigger className="-ml-1" />
      <div aria-hidden className="mr-1 h-5 w-px shrink-0 bg-border" />

      <div className="flex min-w-0 flex-1 items-center gap-2">{children}</div>

      {loadError && (
        <Badge
          variant="outline"
          className="hidden gap-1.5 border-warning/40 text-warning sm:inline-flex"
          role="status"
        >
          <WifiOffIcon />
          Offline, retrying
        </Badge>
      )}

      {actions}

      {/* Sized by the room the page has, not the window: with the design
          open beside a chat, a wide window can still mean a narrow page. */}
      <Button
        variant="outline"
        size="sm"
        onClick={openPalette}
        className="hidden text-muted-foreground @5xl/page:inline-flex"
      >
        <SearchIcon />
        Search
        <Kbd className="ml-2">{shortcut}</Kbd>
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="Search"
        onClick={openPalette}
        className="@5xl/page:hidden"
      >
        <SearchIcon />
      </Button>
    </header>
  );
}
