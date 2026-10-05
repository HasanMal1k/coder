"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { currentToken, type Instance, presenceUrl, renewToken } from "@/lib/api";
import { clearSession, getSession, setSession, tokenExpiresAt } from "@/lib/session";
import { SESSION_EXPIRED } from "@/lib/api";
import { CreateMachineDialog } from "../machines/create-machine-dialog";
import { DeleteMachineDialog } from "../machines/delete-machine-dialog";
import { CommandPalette } from "./command-palette";

type ShellApi = {
  /**
   * How long, in seconds, machines keep running after the user's last
   * connection closes. 0 means they are never stopped for that; null means
   * the server hasn't said yet.
   */
  idleStopSeconds: number | null;
  openCreate: () => void;
  openPalette: () => void;
  /** Asks the user to confirm, then deletes. `onDeleted` runs if it went through. */
  confirmDelete: (instance: Instance, onDeleted?: () => void) => void;
};

const ShellContext = createContext<ShellApi | null>(null);

/** Renew once the token has less than this left. */
const RENEW_WHEN_LEFT_MS = 5 * 60 * 1000;
const RENEW_CHECK_MS = 30 * 1000;

/**
 * Keeps the user signed in while the app is open, by trading the access token
 * for a fresh one shortly before it expires. If the app was asleep past the
 * expiry (a closed laptop), signs out with an explanation.
 */
function useSessionRenewal() {
  useEffect(() => {
    let renewing = false;

    const check = async () => {
      const session = getSession();
      if (!session || renewing) return;

      const left = tokenExpiresAt(session.token) - Date.now();
      if (left <= 0) {
        clearSession(SESSION_EXPIRED);
        return;
      }
      if (left > RENEW_WHEN_LEFT_MS) return;

      renewing = true;
      try {
        setSession(await renewToken());
      } catch {
        // A rejected token signs out by itself; a network blip is retried on
        // the next check.
      } finally {
        renewing = false;
      }
    };

    check();
    const timer = setInterval(check, RENEW_CHECK_MS);
    // Timers are throttled in background tabs, so also check on return.
    document.addEventListener("visibilitychange", check);
    window.addEventListener("focus", check);

    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", check);
      window.removeEventListener("focus", check);
    };
  }, []);
}

const PRESENCE_RETRY_MAX_MS = 30 * 1000;
// The close code the server uses for a token it doesn't accept.
const CLOSE_UNAUTHORIZED = 4401;

/**
 * Holds a connection to the server for as long as this tab is open. The
 * connection carries nothing after its handshake; its existence is what
 * tells the server the user is here. When the last tab closes, the server
 * starts counting down to stopping the user's machines.
 *
 * `onReady` receives how long that countdown is.
 */
function usePresence(onReady: (idleStopSeconds: number) => void) {
  useEffect(() => {
    let socket: WebSocket | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let stopped = false;

    const connect = () => {
      if (stopped) return;

      const ws = new WebSocket(presenceUrl());
      socket = ws;

      ws.onopen = () => {
        try {
          ws.send(JSON.stringify({ type: "auth", token: currentToken() }));
        } catch {
          ws.close();
        }
      };

      ws.onmessage = (event) => {
        if (typeof event.data !== "string") return;
        const message = JSON.parse(event.data) as { type: string; idle_stop_seconds?: number };
        if (message.type === "ready") {
          attempt = 0;
          onReady(message.idle_stop_seconds ?? 0);
        }
      };

      ws.onclose = (event) => {
        if (stopped) return;
        if (event.code === CLOSE_UNAUTHORIZED) {
          clearSession(SESSION_EXPIRED);
          return;
        }

        // Lost, not closed by us: come back, a little less eagerly each
        // time. The server allows for gaps like this before stopping
        // anything.
        const delay = Math.min(PRESENCE_RETRY_MAX_MS, 1000 * 2 ** attempt++);
        retry = setTimeout(connect, delay);
      };
    };

    // Coming back online is the moment to reconnect, not whenever the
    // current wait happens to run out.
    const onOnline = () => {
      if (socket && socket.readyState !== WebSocket.CLOSED) return;
      clearTimeout(retry);
      attempt = 0;
      connect();
    };

    connect();
    window.addEventListener("online", onOnline);

    return () => {
      stopped = true;
      clearTimeout(retry);
      window.removeEventListener("online", onOnline);
      socket?.close();
    };
  }, [onReady]);
}

/**
 * The parts of the app that any page can summon: the create dialog, the
 * delete confirmation and the command palette.
 */
export function ShellProvider({ children }: { children: ReactNode }) {
  useSessionRenewal();

  const [idleStopSeconds, setIdleStopSeconds] = useState<number | null>(null);
  usePresence(setIdleStopSeconds);

  const [createOpen, setCreateOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [deleting, setDeleting] = useState<{ instance: Instance; onDeleted?: () => void } | null>(
    null,
  );

  const openCreate = useCallback(() => setCreateOpen(true), []);
  const openPalette = useCallback(() => setPaletteOpen(true), []);
  const confirmDelete = useCallback(
    (instance: Instance, onDeleted?: () => void) => setDeleting({ instance, onDeleted }),
    [],
  );

  // Ctrl+K / ⌘K opens the palette from anywhere, except while typing in a
  // terminal, where Ctrl+K belongs to the shell (it deletes to end of line).
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== "k" || !(event.ctrlKey || event.metaKey) || event.altKey)
        return;
      if (event.target instanceof Element && event.target.closest(".xterm")) return;

      event.preventDefault();
      setPaletteOpen((open) => !open);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const api = useMemo(
    () => ({ idleStopSeconds, openCreate, openPalette, confirmDelete }),
    [idleStopSeconds, openCreate, openPalette, confirmDelete],
  );

  return (
    <ShellContext.Provider value={api}>
      {children}

      <CreateMachineDialog open={createOpen} onClose={() => setCreateOpen(false)} />
      <DeleteMachineDialog
        instance={deleting?.instance ?? null}
        onDeleted={deleting?.onDeleted}
        onClose={() => setDeleting(null)}
      />
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
    </ShellContext.Provider>
  );
}

export function useShell() {
  const api = useContext(ShellContext);
  if (!api) throw new Error("useShell must be used inside ShellProvider");
  return api;
}
