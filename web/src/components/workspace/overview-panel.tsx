"use client";

import { PlayIcon, SquareIcon, Trash2Icon } from "lucide-react";
import type { ReactNode } from "react";
import type { Instance, InstanceStatus } from "@/lib/api";
import { formatDateTime, formatDuration, formatMemory, formatRelative } from "@/lib/format";
import { CopyButton } from "../copy-button";
import { useShell } from "../shell/shell-provider";
import { StatusBadge } from "../status";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";

/** Everything about a machine that isn't the editor or the terminal. */
export function OverviewPanel({
  instance,
  status,
  busy,
  onStart,
  onStop,
  onDelete,
}: {
  instance: Instance;
  status: InstanceStatus;
  busy: boolean;
  onStart: () => void;
  onStop: () => void;
  onDelete: () => void;
}) {
  const { idleStopSeconds } = useShell();

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 p-4 sm:p-6 lg:p-8">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="min-w-0">
            <h2 className="truncate font-mono text-2xl font-semibold tracking-tight">
              {instance.name}
            </h2>
            <div className="mt-2">
              <StatusBadge status={status} />
            </div>
          </div>
          {status === "running" ? (
            <Button variant="outline" onClick={onStop} disabled={busy}>
              <SquareIcon data-icon="inline-start" />
              Stop machine
            </Button>
          ) : (
            <Button onClick={onStart} disabled={busy}>
              <PlayIcon data-icon="inline-start" />
              Start machine
            </Button>
          )}
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Details</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="divide-y">
              <Row label="Machine ID">
                <span className="flex min-w-0 items-center gap-1">
                  <code className="min-w-0 truncate font-mono text-[13px]">{instance.id}</code>
                  <CopyButton value={instance.id} label="Copy machine ID" />
                </span>
              </Row>
              <Row label="Size">
                {instance.cpus} vCPU, {formatMemory(instance.memory_mb)} memory
              </Row>
              <Row label="Disk">
                {instance.disk_gb} GB. Your home folder is kept when the machine stops.
              </Row>
              <Row label="System">Ubuntu 24.04 with VS Code, a terminal and sudo</Row>
              {idleStopSeconds ? (
                <Row label="Auto-stop">
                  {formatDuration(idleStopSeconds)} after you close coder, unless its terminal or
                  editor is still open somewhere.
                </Row>
              ) : null}
              <Row label="Created">
                {formatDateTime(instance.created_at)}{" "}
                <span className="text-muted-foreground">
                  ({formatRelative(instance.created_at)})
                </span>
              </Row>
              <Row label="Last changed">
                {formatDateTime(instance.updated_at)}{" "}
                <span className="text-muted-foreground">
                  ({formatRelative(instance.updated_at)})
                </span>
              </Row>
            </dl>
          </CardContent>
        </Card>

        <Card className="border-destructive/30">
          <CardHeader>
            <CardTitle>Delete this machine</CardTitle>
            <CardDescription>
              Deleting removes the machine and every file on it, including your home folder. It
              can&apos;t be undone.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button variant="destructive" onClick={onDelete} disabled={busy}>
              <Trash2Icon data-icon="inline-start" />
              Delete machine
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 py-3 text-sm first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:gap-4">
      <dt className="shrink-0 text-muted-foreground sm:w-32">{label}</dt>
      <dd className="min-w-0 flex-1">{children}</dd>
    </div>
  );
}
