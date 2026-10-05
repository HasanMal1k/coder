"use client";

import {
  AppWindowIcon,
  LayoutDashboardIcon,
  NetworkIcon,
  ServerIcon,
  XIcon,
  type LucideIcon,
} from "lucide-react";
import { useState } from "react";
import type { Architecture } from "@/lib/api";
import { useArchitecture } from "@/lib/architecture";
import { useInstances } from "@/lib/instances";
import { cn } from "@/lib/utils";
import { useAssistant, type PaneTab } from "../assistant/assistant-provider";
import { Button } from "../ui/button";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "../ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { BackendTab } from "./backend-tab";
import { DashboardTab } from "./dashboard-tab";
import { FrontendTab } from "./frontend-tab";
import { GraphTab } from "./graph-tab";
import { partsOf } from "./shared";

const TABS: { id: PaneTab; label: string; icon: LucideIcon }[] = [
  { id: "graph", label: "Graph", icon: NetworkIcon },
  { id: "frontend", label: "Frontend", icon: AppWindowIcon },
  { id: "backend", label: "Backend", icon: ServerIcon },
  { id: "dashboard", label: "Dashboard", icon: LayoutDashboardIcon },
];

/**
 * What a chat is building, next to the chat, in four views: the design as a
 * graph, the frontend's page, the backend's API, and how the machines and
 * agents stand. On a small screen there is no room beside the chat, so it
 * slides over it instead.
 */
export function SidePane({ chatId }: { chatId?: string }) {
  const { beside, designOpen, setDesignOpen } = useAssistant();

  if (!beside) {
    return (
      <Sheet open={designOpen} onOpenChange={setDesignOpen}>
        <SheetContent
          side="right"
          showCloseButton={false}
          className="w-full gap-0 bg-background p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl"
        >
          <SheetTitle className="sr-only">Project</SheetTitle>
          <SheetDescription className="sr-only">
            What this chat is building: its design, its page, its API, and its machines.
          </SheetDescription>
          <Pane chatId={chatId} />
        </SheetContent>
      </Sheet>
    );
  }

  if (!designOpen) return null;

  return (
    <aside aria-label="Project" className="flex min-w-0 flex-1 flex-col border-l bg-background">
      <Pane chatId={chatId} />
    </aside>
  );
}

function Pane({ chatId }: { chatId?: string }) {
  if (!chatId) return <Views architecture={null} onChanged={() => {}} />;

  // Keyed by chat, so another chat's design starts from a clean slate.
  return <ChatViews key={chatId} chatId={chatId} />;
}

function ChatViews({ chatId }: { chatId: string }) {
  // Looked up once, here, for every tab: each draws from the same design.
  const { architecture, refresh } = useArchitecture(chatId);

  return <Views chatId={chatId} architecture={architecture} onChanged={refresh} />;
}

function Views({
  chatId,
  architecture,
  onChanged,
}: {
  chatId?: string;
  architecture: Architecture | null | undefined;
  onChanged: () => void;
}) {
  const { paneTab, setPaneTab, setDesignOpen } = useAssistant();
  const { instances } = useInstances();

  // A tab is drawn when first opened and kept from then on, hidden while
  // another shows: the page in Frontend stays where it was, a request half
  // filled in under Backend is still there, and the graph keeps its layout.
  const [opened, setOpened] = useState<PaneTab[]>([paneTab]);
  if (!opened.includes(paneTab)) setOpened([...opened, paneTab]);

  // A dot on a tab whose subject is up, so there is something to go and see.
  const live: Partial<Record<PaneTab, boolean>> = {
    frontend: partsOf(architecture, instances, ["frontend"]).some(
      (p) => p.listening && p.machine?.status === "running",
    ),
    backend: partsOf(architecture, instances, ["backend", "service"]).some(
      (p) => p.listening && p.machine?.status === "running",
    ),
  };

  return (
    <Tabs
      value={paneTab}
      onValueChange={(tab) => setPaneTab(tab as PaneTab)}
      className="@container/pane min-h-0 flex-1 gap-0"
    >
      <header className="flex h-14 shrink-0 items-center gap-2 border-b pr-2 pl-2">
        <TabsList variant="line" className="h-full min-w-0 flex-1 justify-start gap-0 p-0">
          {TABS.map((tab) => (
            <TabsTrigger
              key={tab.id}
              value={tab.id}
              className="h-full flex-none gap-2 rounded-none px-3 group-data-horizontal/tabs:after:bottom-0"
            >
              <tab.icon className="hidden @md/pane:block" />
              {tab.label}
              {live[tab.id] && (
                <span
                  role="img"
                  aria-label="Running"
                  className="size-1.5 rounded-full bg-success"
                />
              )}
            </TabsTrigger>
          ))}
        </TabsList>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Hide the panel"
              onClick={() => setDesignOpen(false)}
            >
              <XIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Hide</TooltipContent>
        </Tooltip>
      </header>

      {TABS.filter((tab) => opened.includes(tab.id)).map((tab) => (
        <TabsContent
          key={tab.id}
          value={tab.id}
          forceMount
          className={cn("flex min-h-0 flex-col data-[state=inactive]:hidden")}
        >
          {tab.id === "graph" && (
            <GraphTab chatId={chatId} architecture={architecture} onChanged={onChanged} />
          )}
          {tab.id === "frontend" && <FrontendTab architecture={architecture} />}
          {tab.id === "backend" && <BackendTab chatId={chatId} architecture={architecture} />}
          {tab.id === "dashboard" && <DashboardTab chatId={chatId} architecture={architecture} />}
        </TabsContent>
      ))}
    </Tabs>
  );
}
