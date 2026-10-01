"use client";

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef, useState } from "react";
import { terminalUrl } from "@/lib/api";
import { clearSession } from "@/lib/session";
import { Button, Spinner } from "./ui";

// Close codes the server uses; see docs/API.md.
const CLOSE_NORMAL = 1000;
const CLOSE_UNAUTHORIZED = 4401;
const CLOSE_NOT_FOUND = 4404;
const CLOSE_NOT_RUNNING = 4409;
const CLOSE_UNREACHABLE = 4503;

// System fonts only: xterm measures character cells when it opens, and a web
// font arriving afterwards would leave the grid misaligned.
const FONT_FAMILY =
  'ui-monospace, "Cascadia Mono", "SF Mono", Menlo, Consolas, "DejaVu Sans Mono", monospace';

const THEME = {
  background: "#07090b",
  foreground: "#e7e9ec",
  cursor: "#3ecf8e",
  cursorAccent: "#07090b",
  selectionBackground: "#3ecf8e55",
};

/** When, after the window loses focus, to check whether the editor's frame took it. */
const RECLAIM_CHECKS_MS = [0, 10, 30, 80, 200];

/** How many keystrokes to hold while the shell is still connecting. */
const MAX_TYPED_AHEAD = 1024;

type Phase =
  | { kind: "connecting" }
  | { kind: "open" }
  | { kind: "closed"; message: string; action: string };

function describeClose(event: CloseEvent, exitCode: number | null): Phase {
  switch (event.code) {
    case CLOSE_NORMAL:
      return {
        kind: "closed",
        message: exitCode === null ? "The shell exited." : `The shell exited with code ${exitCode}.`,
        action: "New shell",
      };
    case CLOSE_UNAUTHORIZED:
      return { kind: "closed", message: "Your session expired.", action: "Reconnect" };
    case CLOSE_NOT_FOUND:
      return { kind: "closed", message: "This machine no longer exists.", action: "Reconnect" };
    case CLOSE_NOT_RUNNING:
      return { kind: "closed", message: "The machine is not running.", action: "Reconnect" };
    case CLOSE_UNREACHABLE:
      return {
        kind: "closed",
        message: "The machine could not be reached. It may still be starting up.",
        action: "Reconnect",
      };
    default:
      return { kind: "closed", message: "The connection was lost.", action: "Reconnect" };
  }
}

/**
 * A terminal connected to a shell on the given machine. `onEnded` is called
 * whenever the connection closes, so the page can re-check the machine.
 */
export default function TerminalView({
  instanceId,
  token,
  active,
  onEnded,
}: {
  instanceId: string;
  token: string;
  /** Whether the terminal is the visible tab. It keeps running when it isn't. */
  active: boolean;
  onEnded: () => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const [phase, setPhase] = useState<Phase>({ kind: "connecting" });
  // Bumping this tears the terminal down and opens a fresh one.
  const [attempt, setAttempt] = useState(0);

  // Kept in refs so a new value doesn't restart the terminal.
  const onEndedRef = useRef(onEnded);
  const activeRef = useRef(active);
  useEffect(() => {
    onEndedRef.current = onEnded;
    activeRef.current = active;
  }, [onEnded, active]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const term = new Terminal({
      cursorBlink: true,
      fontFamily: FONT_FAMILY,
      fontSize: 14,
      scrollback: 10000,
      theme: THEME,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(container);
    fit.fit();
    termRef.current = term;
    if (activeRef.current) term.focus();

    const socket = new WebSocket(terminalUrl(instanceId));
    socket.binaryType = "arraybuffer";
    const encoder = new TextEncoder();
    let exitCode: number | null = null;
    // Keystrokes typed before the shell is ready are held here and delivered
    // once it is, rather than lost. null once the shell is ready.
    let typedAhead: string[] | null = [];
    // Set once this effect is cleaned up, so late events from this socket
    // can't touch state that now belongs to its replacement.
    let disposed = false;

    socket.onopen = () => {
      // Browsers can't set headers on a WebSocket, so the token goes in the
      // first message instead.
      socket.send(JSON.stringify({ type: "auth", token, cols: term.cols, rows: term.rows }));
    };

    socket.onmessage = (event) => {
      if (disposed) return;

      if (typeof event.data !== "string") {
        term.write(new Uint8Array(event.data as ArrayBuffer));
        return;
      }

      const message = JSON.parse(event.data) as { type: string; code?: number };
      if (message.type === "ready") {
        setPhase({ kind: "open" });
        for (const data of typedAhead ?? []) socket.send(encoder.encode(data));
        typedAhead = null;
      } else if (message.type === "exit") {
        exitCode = message.code ?? null;
      }
    };

    socket.onclose = (event) => {
      if (disposed) return;

      setPhase(describeClose(event, exitCode));
      if (event.code === CLOSE_UNAUTHORIZED) {
        clearSession("Your session expired. Sign in again to continue.");
      }
      onEndedRef.current();
    };

    const onData = term.onData((data) => {
      if (typedAhead) {
        // Bounded, so holding a key down while disconnected can't grow it
        // without limit.
        if (typedAhead.length < MAX_TYPED_AHEAD) typedAhead.push(data);
      } else if (socket.readyState === WebSocket.OPEN) {
        socket.send(encoder.encode(data));
      }
    });
    const onResize = term.onResize(({ cols, rows }) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "resize", cols, rows }));
      }
    });

    // Refit when the container changes size; xterm then reports the new
    // dimensions through onResize.
    // While its tab is hidden the container has no size; fitting then would
    // shrink the shell to nothing, so wait until it is shown again.
    let frame = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (container.clientWidth > 0 && container.clientHeight > 0) fit.fit();
      });
    });
    observer.observe(container);

    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      observer.disconnect();
      onData.dispose();
      onResize.dispose();
      socket.close();
      term.dispose();
      termRef.current = null;
    };
  }, [instanceId, token, attempt]);

  // While this is the visible tab, typing belongs to the terminal.
  useEffect(() => {
    if (!active) return;

    termRef.current?.focus();

    // The editor lives in a frame on the other tab, and takes keyboard focus
    // when it finishes loading, even though it is hidden. The window losing
    // focus to a frame is how that shows up here; take it back, or the user's
    // keystrokes would go to an editor they can't see.
    //
    // This can't be done inside the blur event itself. The browser is still
    // handing focus to the frame at that point; focusing the terminal then is
    // overridden when the hand-over completes, leaving the page believing
    // the terminal is focused while keys go to the editor. Nor is one check
    // on the next tick enough: focus passes through "nothing focused" on its
    // way to the frame, and a check that lands there sees no frame yet. So
    // look a few times in quick succession.
    let timers: ReturnType<typeof setTimeout>[] = [];
    const take = () => {
      if (document.activeElement instanceof HTMLIFrameElement) {
        window.focus();
        termRef.current?.focus();
      }
    };
    const reclaim = () => {
      timers.forEach(clearTimeout);
      timers = RECLAIM_CHECKS_MS.map((delay) => setTimeout(take, delay));
    };
    window.addEventListener("blur", reclaim);

    return () => {
      window.removeEventListener("blur", reclaim);
      timers.forEach(clearTimeout);
    };
  }, [active]);

  return (
    <div className="relative flex min-h-0 flex-1 flex-col bg-term p-2">
      <div ref={containerRef} className="min-h-0 flex-1" />

      {phase.kind === "connecting" && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center gap-3 text-sm text-muted">
          <Spinner />
          Connecting to your machine
        </div>
      )}

      {phase.kind === "closed" && (
        <div className="absolute inset-x-0 bottom-0 flex flex-wrap items-center justify-between gap-3 border-t border-line bg-surface px-4 py-3 text-sm">
          <span role="status">{phase.message}</span>
          <Button
            variant="primary"
            onClick={() => {
              setPhase({ kind: "connecting" });
              setAttempt((n) => n + 1);
            }}
          >
            {phase.action}
          </Button>
        </div>
      )}
    </div>
  );
}
