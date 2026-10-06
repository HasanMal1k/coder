"use client";

import {
  ArrowRightIcon,
  CopyIcon,
  MonitorIcon,
  MoreHorizontalIcon,
  PlayIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
  SquareIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import type { Instance } from "@/lib/api";
import {
  formatDateTime,
  formatDuration,
  formatMemory,
  formatRelative,
  formatSpecs,
  formatStack,
} from "@/lib/format";
import { displayStatus, isBusy, useInstances, type PendingAction } from "@/lib/instances";
import { useTitle } from "@/lib/use-title";
import { cn } from "@/lib/utils";
import { AgentsSection } from "../agents/agents-section";
import { PageHeader } from "../shell/page-header";
import { useShell } from "../shell/shell-provider";
import { Stat, Stats } from "../stats";
import { StatusBadge } from "../status";
import { Alert, AlertAction, AlertDescription } from "../ui/alert";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Card } from "../ui/card";
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
import { InputGroup, InputGroupAddon, InputGroupInput } from "../ui/input-group";
import { Skeleton } from "../ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import { Tabs, TabsList, TabsTrigger } from "../ui/tabs";

type Filter = "all" | "running" | "stopped";

const FILTERS: { id: Filter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "running", label: "Running" },
  { id: "stopped", label: "Stopped" },
];

export function MachinesPage() {
  useTitle("Machines");

  const { instances, loadError, pending, refresh } = useInstances();
  const { openCreate, idleStopSeconds } = useShell();

  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [refreshing, setRefreshing] = useState(false);

  const counts = useMemo(() => {
    const list = instances ?? [];
    const running = list.filter((i) => i.status === "running");
    return {
      all: list.length,
      running: running.length,
      stopped: list.length - running.length,
      cpusRunning: running.reduce((sum, i) => sum + i.cpus, 0),
      cpusTotal: list.reduce((sum, i) => sum + i.cpus, 0),
      memoryRunning: running.reduce((sum, i) => sum + i.memory_mb, 0),
      memoryTotal: list.reduce((sum, i) => sum + i.memory_mb, 0),
    };
  }, [instances]);

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (instances ?? []).filter((i) => {
      if (needle && !i.name.includes(needle)) return false;
      if (filter === "running") return i.status === "running";
      if (filter === "stopped") return i.status !== "running";
      return true;
    });
  }, [instances, query, filter]);

  async function onRefresh() {
    setRefreshing(true);
    await refresh();
    setRefreshing(false);
  }

  const loading = instances === null && !loadError;

  return (
    <>
      <PageHeader>
        <h1 className="truncate text-sm font-medium">Machines</h1>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 sm:p-6 lg:p-8">
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div className="min-w-0">
              <h2 className="text-2xl font-semibold tracking-tight">Machines</h2>
              <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
                Each machine is your own Linux computer, with VS Code and a terminal in the browser.
                {idleStopSeconds ? (
                  <>
                    {" "}
                    Machines stop by themselves {formatDuration(idleStopSeconds)} after you close
                    coder; your files are kept.
                  </>
                ) : null}
              </p>
            </div>
            <Button onClick={openCreate}>
              <PlusIcon data-icon="inline-start" />
              Create machine
            </Button>
          </div>

          {loadError && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertDescription>
                {loadError}
                {instances !== null && " Showing what was last loaded."}
              </AlertDescription>
              <AlertAction>
                <Button variant="outline" size="xs" onClick={onRefresh} disabled={refreshing}>
                  Retry
                </Button>
              </AlertAction>
            </Alert>
          )}

          {/* Four zeros tell a new user nothing. */}
          {instances?.length !== 0 && (
            <Stats className="@3xl/page:grid-cols-4">
              <Stat label="Machines" value={loading ? null : String(counts.all)} />
              <Stat
                label="Running"
                value={loading ? null : String(counts.running)}
                detail={`of ${counts.all}`}
                tone={counts.running > 0 ? "text-success" : undefined}
              />
              <Stat
                label="vCPUs in use"
                value={loading ? null : String(counts.cpusRunning)}
                detail={`of ${counts.cpusTotal}`}
              />
              <Stat
                label="Memory in use"
                value={loading ? null : formatMemory(counts.memoryRunning)}
                detail={`of ${formatMemory(counts.memoryTotal)}`}
              />
            </Stats>
          )}

          <AgentsSection />

          {instances?.length === 0 ? (
            <Empty className="border py-16">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <MonitorIcon />
                </EmptyMedia>
                <EmptyTitle>Create your first machine</EmptyTitle>
                <EmptyDescription>
                  It takes a few seconds. You get a Linux machine with root access, VS Code and a
                  terminal, and your files stay put when it stops.
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={openCreate}>
                  <PlusIcon data-icon="inline-start" />
                  Create machine
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <section aria-label="Your machines" className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center gap-2">
                <InputGroup className="w-full sm:w-64">
                  <InputGroupAddon>
                    <SearchIcon />
                  </InputGroupAddon>
                  <InputGroupInput
                    type="search"
                    aria-label="Filter machines by name"
                    placeholder="Filter by name"
                    autoComplete="off"
                    spellCheck={false}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                  />
                </InputGroup>

                <Tabs value={filter} onValueChange={(value) => setFilter(value as Filter)}>
                  <TabsList aria-label="Filter by status">
                    {FILTERS.map((option) => (
                      <TabsTrigger key={option.id} value={option.id}>
                        {option.label}
                        <span className="tabular-nums opacity-60">{counts[option.id]}</span>
                      </TabsTrigger>
                    ))}
                  </TabsList>
                </Tabs>

                <span className="flex-1" />
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Refresh"
                  onClick={onRefresh}
                  disabled={refreshing}
                >
                  <RefreshCwIcon className={cn(refreshing && "animate-spin")} />
                </Button>
              </div>

              <Card className="overflow-hidden py-0">
                {(loading || visible.length > 0) && (
                  <Table>
                    <TableHeader>
                      <TableRow className="hover:bg-transparent">
                        <TableHead className="pl-5">Name</TableHead>
                        <TableHead>Status</TableHead>
                        <TableHead className="hidden @2xl/page:table-cell">Size</TableHead>
                        <TableHead className="hidden @2xl/page:table-cell">Created</TableHead>
                        <TableHead className="pr-4">
                          <span className="sr-only">Actions</span>
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {loading &&
                        [0, 1, 2].map((row) => (
                          <TableRow key={row} className="hover:bg-transparent">
                            <TableCell className="py-4 pl-5">
                              <Skeleton className="h-4 w-36" />
                            </TableCell>
                            <TableCell>
                              <Skeleton className="h-5 w-20 rounded-full" />
                            </TableCell>
                            <TableCell className="hidden @2xl/page:table-cell">
                              <Skeleton className="h-4 w-40" />
                            </TableCell>
                            <TableCell className="hidden @2xl/page:table-cell">
                              <Skeleton className="h-4 w-20" />
                            </TableCell>
                            <TableCell />
                          </TableRow>
                        ))}

                      {visible.map((instance) => (
                        <MachineRow
                          key={instance.id}
                          instance={instance}
                          pending={pending[instance.id]}
                        />
                      ))}
                    </TableBody>
                  </Table>
                )}

                {instances !== null && visible.length === 0 && (
                  <div className="flex flex-col items-center gap-3 px-4 py-12 text-center">
                    <p className="text-sm text-muted-foreground">No machines match your filters.</p>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setQuery("");
                        setFilter("all");
                      }}
                    >
                      Clear filters
                    </Button>
                  </div>
                )}
              </Card>
            </section>
          )}
        </div>
      </div>
    </>
  );
}

function MachineRow({ instance, pending }: { instance: Instance; pending?: PendingAction }) {
  const { start, stop } = useInstances();
  const { confirmDelete } = useShell();

  const status = displayStatus(instance, pending);
  const busy = isBusy(status);
  const href = `/instances/${instance.id}`;

  async function copyId() {
    try {
      await navigator.clipboard.writeText(instance.id);
      toast("Machine ID copied.");
    } catch {
      toast.error("Couldn't copy to the clipboard.");
    }
  }

  return (
    <TableRow className="group">
      <TableCell className="max-w-0 py-3 pl-5">
        <Link
          href={href}
          className="flex min-w-0 items-center gap-3 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="grid size-8 shrink-0 place-items-center rounded-md border bg-muted text-muted-foreground group-hover:border-foreground/40 group-hover:text-foreground">
            <MonitorIcon className="size-4" />
          </span>
          <span className="min-w-0">
            <span className="flex min-w-0 items-center gap-2">
              <span className="truncate font-mono text-sm font-semibold">{instance.name}</span>
              {formatStack(instance) && (
                <Badge variant="outline" className="shrink-0 text-muted-foreground">
                  {formatStack(instance)}
                </Badge>
              )}
            </span>
            <span className="block truncate text-xs text-muted-foreground @2xl/page:hidden">
              {formatSpecs(instance)}
            </span>
          </span>
        </Link>
      </TableCell>
      <TableCell className="w-px">
        <StatusBadge status={status} />
      </TableCell>
      <TableCell className="hidden w-px whitespace-nowrap text-muted-foreground @2xl/page:table-cell">
        {formatSpecs(instance)}
      </TableCell>
      <TableCell
        className="hidden w-px whitespace-nowrap text-muted-foreground @2xl/page:table-cell"
        title={formatDateTime(instance.created_at)}
      >
        {formatRelative(instance.created_at)}
      </TableCell>
      <TableCell className="w-px pr-4">
        <div className="flex items-center justify-end gap-1.5">
          {/* On a phone these give way to the name; the same actions are in
              the row's menu, and the name itself opens the machine. */}
          {status === "running" && (
            <Button asChild variant="secondary" size="sm" className="@max-lg/page:hidden">
              <Link href={href}>Open</Link>
            </Button>
          )}
          {(status === "stopped" || status === "error") && (
            <Button
              variant="outline"
              size="sm"
              className="@max-lg/page:hidden"
              onClick={() => start(instance.id)}
            >
              <PlayIcon data-icon="inline-start" />
              Start
            </Button>
          )}

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`More actions for ${instance.name}`}
              >
                <MoreHorizontalIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="min-w-48">
              <DropdownMenuItem asChild>
                <Link href={href}>
                  <ArrowRightIcon />
                  Open
                </Link>
              </DropdownMenuItem>
              {status === "running" ? (
                <DropdownMenuItem disabled={busy} onSelect={() => stop(instance.id)}>
                  <SquareIcon />
                  Stop
                </DropdownMenuItem>
              ) : (
                <DropdownMenuItem disabled={busy} onSelect={() => start(instance.id)}>
                  <PlayIcon />
                  Start
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onSelect={copyId}>
                <CopyIcon />
                Copy machine ID
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                disabled={busy}
                onSelect={() => confirmDelete(instance)}
              >
                <Trash2Icon />
                Delete…
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </TableCell>
    </TableRow>
  );
}
