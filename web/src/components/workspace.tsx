"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import { useCallback, useState } from "react";
import {
  ApiError,
  errorMessage,
  getInstance,
  IDE_PORT,
  portUrl,
  startInstance,
  type Instance,
} from "@/lib/api";
import type { Session } from "@/lib/session";
import { usePolling } from "@/lib/use-polling";
import { AppHeader } from "./app-header";
import { IDE_PATH, IdeView } from "./ide-view";
import { StatusBadge } from "./status-badge";
import { Button, buttonClass, ErrorNote, PageSpinner, Spinner } from "./ui";

// xterm needs the browser, so it is never rendered on the server.
const TerminalView = dynamic(() => import("./terminal-view"), {
  ssr: false,
  loading: () => <PageSpinner label="Loading terminal" />,
});

const REFRESH_INTERVAL_MS = 5000;

type Tab = "code" | "terminal";

export function Workspace({ session, id }: { session: Session; id: string }) {
  const { token } = session;

  // null while loading; "missing" when the machine doesn't exist (or isn't ours).
  const [instance, setInstance] = useState<Instance | "missing" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [tab, setTab] = useState<Tab>("code");
  // The terminal's shell starts the first time its tab is opened, and then
  // keeps running while the user is on the other tab.
  const [terminalOpened, setTerminalOpened] = useState(false);

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
      const url = await portUrl(token, id, IDE_PORT, IDE_PATH);
      if (opened) opened.location.href = url;
    } catch (err) {
      opened?.close();
      setError(errorMessage(err));
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
        <>
          <div className="flex h-10 shrink-0 items-center gap-1 border-b border-line bg-surface px-2 sm:px-4">
            <div role="tablist" aria-label="Machine views" className="flex h-full items-stretch gap-1">
              <TabButton selected={tab === "code"} onClick={() => selectTab("code")}>
                Code
              </TabButton>
              <TabButton selected={tab === "terminal"} onClick={() => selectTab("terminal")}>
                Terminal
              </TabButton>
            </div>
            <span className="flex-1" />
            {error && <span className="truncate text-xs text-danger">{error}</span>}
            <button
              type="button"
              onClick={openEditorInNewTab}
              className="rounded px-2 py-1 text-xs whitespace-nowrap text-muted outline-none hover:text-fg focus-visible:ring-2 focus-visible:ring-accent/70"
            >
              Open editor in new tab ↗
            </button>
          </div>

          {/* Both views stay mounted so switching tabs loses nothing: the
              editor keeps its open files, the terminal its running shell.

              The inactive one is hidden with display: none. A hidden editor
              can still take keyboard focus when it finishes loading; the
              terminal watches for that and takes it back. */}
          <div className={tab === "code" ? "flex min-h-0 flex-1 flex-col" : "hidden"}>
            <IdeView instanceId={id} token={token} />
          </div>
          {terminalOpened && (
            <div className={tab === "terminal" ? "flex min-h-0 flex-1 flex-col" : "hidden"}>
              <TerminalView
                instanceId={id}
                token={token}
                active={tab === "terminal"}
                onEnded={refresh}
              />
            </div>
          )}
        </>
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

function TabButton({
  selected,
  onClick,
  children,
}: {
  selected: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      onClick={onClick}
      className={`border-b-2 px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-accent/70 ${
        selected ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg"
      }`}
    >
      {children}
    </button>
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
