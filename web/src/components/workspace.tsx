"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import { useCallback, useState } from "react";
import { ApiError, errorMessage, getInstance, startInstance, type Instance } from "@/lib/api";
import type { Session } from "@/lib/session";
import { usePolling } from "@/lib/use-polling";
import { AppHeader } from "./app-header";
import { StatusBadge } from "./status-badge";
import { Button, buttonClass, ErrorNote, PageSpinner, Spinner } from "./ui";

// xterm needs the browser, so it is never rendered on the server.
const TerminalView = dynamic(() => import("./terminal-view"), {
  ssr: false,
  loading: () => <PageSpinner label="Loading terminal" />,
});

const REFRESH_INTERVAL_MS = 5000;

export function Workspace({ session, id }: { session: Session; id: string }) {
  const { token } = session;

  // null while loading; "missing" when the machine doesn't exist (or isn't ours).
  const [instance, setInstance] = useState<Instance | "missing" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const latest = await getInstance(token, id);
      setInstance(latest);
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setInstance("missing");
      } else {
        setError(errorMessage(err));
      }
    }
  }, [token, id]);

  usePolling(refresh, REFRESH_INTERVAL_MS);

  async function start() {
    setStarting(true);
    setError(null);
    try {
      setInstance(await startInstance(token, id));
    } catch (err) {
      setError(errorMessage(err));
      refresh();
    } finally {
      setStarting(false);
    }
  }

  const known = instance !== null && instance !== "missing" ? instance : null;
  const status = starting ? "starting" : known?.status;

  return (
    // Exactly one viewport tall, so the terminal fills what the header leaves
    // and scrolls inside itself rather than scrolling the page.
    <div className="flex h-dvh flex-col">
      <AppHeader session={session}>
        <span className="text-line-strong">/</span>
        <Link href="/" className="text-sm text-muted hover:text-fg">
          Machines
        </Link>
        {known && status && (
          <>
            <span className="text-line-strong">/</span>
            <span className="truncate font-mono text-sm font-semibold">{known.name}</span>
            <StatusBadge status={status} />
          </>
        )}
      </AppHeader>

      {instance === null ? (
        error ? (
          <Notice>
            <ErrorNote>{error}</ErrorNote>
          </Notice>
        ) : (
          <PageSpinner label="Loading machine" />
        )
      ) : instance === "missing" ? (
        <Notice title="Machine not found">
          <p className="text-sm text-muted">It may have been deleted.</p>
          <Link href="/" className={buttonClass("secondary")}>
            Back to machines
          </Link>
        </Notice>
      ) : status === "running" ? (
        <TerminalView instanceId={id} token={token} onEnded={refresh} />
      ) : status === "stopped" || status === "error" ? (
        <Notice title={status === "error" ? "This machine hit an error" : "This machine is stopped"}>
          <p className="text-sm text-muted">
            {status === "error"
              ? "The last operation failed. Starting it again usually fixes that."
              : "Start it to open a terminal. Your files are still there."}
          </p>
          {error && <ErrorNote>{error}</ErrorNote>}
          <Button variant="primary" onClick={start}>
            Start machine
          </Button>
        </Notice>
      ) : (
        <Notice>
          <p className="flex items-center gap-3 text-sm text-muted" role="status">
            <Spinner />
            Machine is {status}
          </p>
        </Notice>
      )}
    </div>
  );
}

function Notice({ title, children }: { title?: string; children: React.ReactNode }) {
  return (
    <main className="flex flex-1 items-center justify-center px-4">
      <div className="flex max-w-sm flex-col items-center gap-4 text-center">
        {title && <h1 className="text-lg font-semibold">{title}</h1>}
        {children}
      </div>
    </main>
  );
}
