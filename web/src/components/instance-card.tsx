"use client";

import Link from "next/link";
import { useState } from "react";
import type { Instance, InstanceStatus } from "@/lib/api";
import { formatRelative, formatSpecs } from "@/lib/format";
import { StatusBadge } from "./status-badge";
import { Button, buttonClass } from "./ui";

/** An operation this page has asked for and is waiting on. */
export type PendingAction = "starting" | "stopping" | "deleting";

const BUSY: InstanceStatus[] = ["creating", "starting", "stopping", "deleting"];

export function InstanceCard({
  instance,
  pending,
  error,
  onStart,
  onStop,
  onDelete,
}: {
  instance: Instance;
  pending?: PendingAction;
  error?: string;
  onStart: () => void;
  onStop: () => void;
  onDelete: () => void;
}) {
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  // While we wait on our own request, show where it is heading.
  const status = pending ?? instance.status;
  const busy = BUSY.includes(status);

  return (
    <li className="rounded-xl border border-line bg-surface p-4 sm:p-5">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h2 className="truncate font-mono text-base font-semibold">{instance.name}</h2>
            <StatusBadge status={status} />
          </div>
          <p className="mt-1.5 text-sm text-muted">
            {formatSpecs(instance)} · created {formatRelative(instance.created_at)}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {confirmingDelete ? (
            <>
              <span className="text-sm text-muted">Delete this machine and all its files?</span>
              <Button
                variant="danger"
                onClick={() => {
                  setConfirmingDelete(false);
                  onDelete();
                }}
              >
                Delete forever
              </Button>
              <Button onClick={() => setConfirmingDelete(false)}>Cancel</Button>
            </>
          ) : (
            <>
              {status === "running" && (
                <Link href={`/instances/${instance.id}`} className={buttonClass("primary")}>
                  Open terminal
                </Link>
              )}
              {(status === "stopped" || status === "error") && (
                <Button variant="primary" onClick={onStart}>
                  Start
                </Button>
              )}
              {status === "running" && <Button onClick={onStop}>Stop</Button>}
              <Button variant="ghost" disabled={busy} onClick={() => setConfirmingDelete(true)}>
                Delete
              </Button>
            </>
          )}
        </div>
      </div>

      {error && (
        <p role="alert" className="mt-3 text-sm text-danger">
          {error}
        </p>
      )}
      {!error && status === "error" && (
        <p className="mt-3 text-sm text-muted">
          The last operation on this machine failed. Start it to try again, or delete it.
        </p>
      )}
    </li>
  );
}
