"use client";

import {
  ChevronsUpDownIcon,
  CodeXmlIcon,
  CopyIcon,
  ExternalLinkIcon,
  InfoIcon,
  ListIcon,
  MonitorIcon,
  MonitorOffIcon,
  MoreHorizontalIcon,
  PlayIcon,
  PlusIcon,
  SquareIcon,
  SquareTerminalIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import { toast } from "sonner";
import { errorMessage, IDE_PORT, portUrl, type Instance, type InstanceStatus } from "@/lib/api";
import { displayStatus, isBusy, useInstances } from "@/lib/instances";
import { useTitle } from "@/lib/use-title";
import { cn } from "@/lib/utils";
import { chatHref, useAssistant } from "../assistant/assistant-provider";
import { PageHeader } from "../shell/page-header";
import { useShell } from "../shell/shell-provider";
import { StatusBadge, StatusDot, statusLabel } from "../status";
import { Alert, AlertAction, AlertDescription } from "../ui/alert";
import { Button } from "../ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "../ui/empty";
import { Spinner } from "../ui/spinner";
import { Tabs, TabsList, TabsTrigger } from "../ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { IDE_PATH, IdeView } from "./ide-view";
import { OverviewPanel } from "./overview-panel";

// xterm needs the browser, so it is never rendered on the server.
const TerminalView = dynamic(() => import("./terminal-view"), {
  ssr: false,
  loading: () => (
    <div
      className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground"
      role="status"
    >
      <Spinner />
      Loading terminal
    </div>
  ),
});

type Tab = "code" | "terminal" | "overview";

const TABS = [
  { id: "code", label: "Code", icon: CodeXmlIcon },
  { id: "terminal", label: "Terminal", icon: SquareTerminalIcon },
  { id: "overview", label: "Overview", icon: InfoIcon },
] as const;

/** One machine's page: its editor, its terminal, and its details. */
export function Workspace({ id }: { id: string }) {
  const router = useRouter();
  const { instances, loadError, pending, refresh, start, stop } = useInstances();
  const { confirmDelete, openCreate } = useShell();
  const { chats, lastChatId } = useAssistant();
  // The chat the user was in before coming here, if it is still around: the
  // way back to it is the first thing in the header.
  const cameFrom = chats?.find((chat) => chat.id === lastChatId);

  const instance = instances?.find((i) => i.id === id) ?? null;
  const status = instance ? displayStatus(instance, pending[id]) : null;
  const busy = status !== null && isBusy(status);

  useTitle(instance?.name ?? "Machine");

  const [tab, setTab] = useState<Tab>("code");
  // The terminal's shell starts the first time its tab is opened, and then
  // keeps running while the user is on another tab.
  const [terminalOpened, setTerminalOpened] = useState(false);

  function selectTab(next: Tab) {
    setTab(next);
    if (next === "terminal") setTerminalOpened(true);
  }

  /** Opens the editor in its own browser tab. */
  async function openEditorInNewTab() {
    // Open the tab now, while the browser still counts this as the user's
    // click; a window opened after waiting for the network gets blocked.
    const opened = window.open("", "_blank");
    try {
      const url = await portUrl(id, IDE_PORT, IDE_PATH);
      if (opened) opened.location.href = url;
    } catch (err) {
      opened?.close();
      toast.error("Couldn't open the editor", { description: errorMessage(err) });
    }
  }

  async function copyId() {
    try {
      await navigator.clipboard.writeText(id);
      toast("Machine ID copied.");
    } catch {
      toast.error("Couldn't copy to the clipboard.");
    }
  }

  const askDelete = () => instance && confirmDelete(instance, () => router.replace("/machines"));
  const others = (instances ?? []).filter((i) => i.id !== id);

  return (
    <>
      <PageHeader
        actions={
          instance &&
          status && (
            <div className="flex items-center gap-1.5">
              <StatusBadge status={status} className="hidden @xl/page:inline-flex" />

              {status === "running" && (
                <>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label="Open editor in new tab"
                        className="@max-lg/page:hidden"
                        onClick={openEditorInNewTab}
                      >
                        <ExternalLinkIcon />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Open editor in new tab</TooltipContent>
                  </Tooltip>
                  <Button variant="outline" size="sm" onClick={() => stop(id)}>
                    <SquareIcon data-icon="inline-start" />
                    Stop
                  </Button>
                </>
              )}
              {(status === "stopped" || status === "error") && (
                <Button size="sm" onClick={() => start(id)}>
                  <PlayIcon data-icon="inline-start" />
                  Start
                </Button>
              )}

              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-sm" aria-label="More actions">
                    <MoreHorizontalIcon />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="min-w-48">
                  {status === "running" && (
                    <DropdownMenuItem onSelect={openEditorInNewTab}>
                      <ExternalLinkIcon />
                      Open editor in new tab
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem onSelect={copyId}>
                    <CopyIcon />
                    Copy machine ID
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" disabled={busy} onSelect={askDelete}>
                    <Trash2Icon />
                    Delete…
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          )
        }
      >
        <Link
          href={cameFrom ? chatHref(cameFrom.id) : "/machines"}
          title={cameFrom ? `Back to the chat "${cameFrom.title}"` : undefined}
          className="hidden max-w-56 shrink-0 truncate rounded-md text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring @2xl/page:block"
        >
          {cameFrom ? cameFrom.title : "Machines"}
        </Link>
        <span aria-hidden className="hidden text-muted-foreground/50 @2xl/page:block">
          /
        </span>

        {instance ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                aria-label="Switch machine"
                className="min-w-0 px-2"
              >
                <span className="truncate font-mono font-semibold">{instance.name}</span>
                <ChevronsUpDownIcon className="text-muted-foreground" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="min-w-56">
              {others.map((other) => (
                <DropdownMenuItem key={other.id} asChild>
                  <Link href={`/instances/${other.id}`}>
                    <StatusDot status={displayStatus(other, pending[other.id])} className="mx-1" />
                    <span className="font-mono">{other.name}</span>
                  </Link>
                </DropdownMenuItem>
              ))}
              {others.length > 0 && <DropdownMenuSeparator />}
              <DropdownMenuItem asChild>
                <Link href="/machines">
                  <ListIcon />
                  All machines
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={openCreate}>
                <PlusIcon />
                Create a machine…
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <span className="text-sm font-medium">Machine</span>
        )}

        {instance && (
          <ViewTabs tab={tab} onSelect={selectTab} className="ml-1 @max-3xl/page:hidden" />
        )}
      </PageHeader>

      {/* On small screens the header has no room for the tabs, so they get
          a row of their own. */}
      {instance && (
        <div className="flex shrink-0 justify-center border-b px-3 py-2 @3xl/page:hidden">
          <ViewTabs tab={tab} onSelect={selectTab} />
        </div>
      )}

      {instance && status ? (
        <div className="flex min-h-0 flex-1 flex-col">
          {/* The editor and terminal stay mounted while the machine runs, so
              switching tabs loses nothing: the editor keeps its open files,
              the terminal its running shell.

              The inactive one is hidden with display: none. A hidden editor
              can still take keyboard focus when it finishes loading; the
              terminal watches for that and takes it back. */}
          {status === "running" ? (
            <>
              <Pane shown={tab === "code"}>
                <IdeView instanceId={id} />
              </Pane>
              {terminalOpened && (
                <Pane shown={tab === "terminal"}>
                  <TerminalView instanceId={id} active={tab === "terminal"} onEnded={refresh} />
                </Pane>
              )}
            </>
          ) : (
            tab !== "overview" && (
              <NotRunning
                instance={instance}
                status={status}
                onStart={() => start(id)}
                onDelete={askDelete}
              />
            )
          )}

          {tab === "overview" && (
            <OverviewPanel
              instance={instance}
              status={status}
              busy={busy}
              onStart={() => start(id)}
              onStop={() => stop(id)}
              onDelete={askDelete}
            />
          )}
        </div>
      ) : instances === null ? (
        loadError ? (
          <div className="flex flex-1 items-center justify-center p-6">
            <Alert variant="destructive" className="max-w-md">
              <TriangleAlertIcon />
              <AlertDescription>{loadError}</AlertDescription>
              <AlertAction>
                <Button variant="outline" size="xs" onClick={refresh}>
                  Retry
                </Button>
              </AlertAction>
            </Alert>
          </div>
        ) : (
          <div
            className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground"
            role="status"
          >
            <Spinner />
            Loading machine
          </div>
        )
      ) : (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <MonitorOffIcon />
            </EmptyMedia>
            <EmptyTitle>Machine not found</EmptyTitle>
            <EmptyDescription>It may have been deleted, or the link is wrong.</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button asChild variant="outline">
              <Link href="/machines">Back to machines</Link>
            </Button>
          </EmptyContent>
        </Empty>
      )}
    </>
  );
}

function ViewTabs({
  tab,
  onSelect,
  className,
}: {
  tab: Tab;
  onSelect: (tab: Tab) => void;
  className?: string;
}) {
  return (
    <Tabs value={tab} onValueChange={(value) => onSelect(value as Tab)} className={className}>
      <TabsList aria-label="Machine views">
        {TABS.map((t) => (
          <TabsTrigger key={t.id} value={t.id}>
            <t.icon />
            {t.label}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}

function Pane({ shown, children }: { shown: boolean; children: ReactNode }) {
  return <div className={cn(shown ? "flex min-h-0 flex-1 flex-col" : "hidden")}>{children}</div>;
}

/** Shown in place of the editor or terminal when the machine isn't running. */
function NotRunning({
  instance,
  status,
  onStart,
  onDelete,
}: {
  instance: Instance;
  status: InstanceStatus;
  onStart: () => void;
  onDelete: () => void;
}) {
  if (isBusy(status)) {
    return (
      <div
        className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground"
        role="status"
      >
        <Spinner />
        {statusLabel(status)} {instance.name}…
      </div>
    );
  }

  const failed = status === "error";

  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          {failed ? <TriangleAlertIcon className="text-destructive" /> : <MonitorIcon />}
        </EmptyMedia>
        <EmptyTitle>
          {failed ? `${instance.name} hit a problem` : `${instance.name} is stopped`}
        </EmptyTitle>
        <EmptyDescription>
          {failed
            ? "The last operation on this machine failed. Starting it again usually fixes that."
            : "Start it to use the editor and the terminal. Your files are still there."}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <div className="flex gap-2">
          <Button onClick={onStart}>
            <PlayIcon data-icon="inline-start" />
            Start machine
          </Button>
          {failed && (
            <Button variant="outline" onClick={onDelete}>
              <Trash2Icon data-icon="inline-start" />
              Delete…
            </Button>
          )}
        </div>
      </EmptyContent>
    </Empty>
  );
}
