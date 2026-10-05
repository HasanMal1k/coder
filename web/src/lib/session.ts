import { useSyncExternalStore } from "react";
import type { User } from "./api";

export type Session = { token: string; user: User };

const SESSION_KEY = "coder.session";
const RETURN_TO_KEY = "coder.returnTo";

// undefined means "not read from storage yet".
let cached: Session | null | undefined;
const listeners = new Set<() => void>();

// Why the user was signed out, for the login page to show once. Kept in
// memory only: it is meant for the navigation that follows, not for later.
let notice: string | null = null;

// Set when the user chose to sign out, as opposed to being signed out. They
// asked to leave, so the next sign-in shouldn't drop them back where they
// were (it may not even be the same person).
let leftOnPurpose = false;

/**
 * When a JWT expires, in milliseconds since the epoch: Infinity for a token
 * that never does (it carries no expiry), 0 if the token can't be read.
 */
export function tokenExpiresAt(token: string) {
  try {
    const payload = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
    const { exp } = JSON.parse(atob(payload)) as { exp?: number };
    return typeof exp === "number" ? exp * 1000 : Infinity;
  } catch {
    return 0;
  }
}

function read(): Session | null {
  try {
    const raw = localStorage.getItem(SESSION_KEY);
    if (!raw) return null;

    const session = JSON.parse(raw) as Session;
    if (!session.token || !session.user || tokenExpiresAt(session.token) <= Date.now()) {
      localStorage.removeItem(SESSION_KEY);
      return null;
    }
    return session;
  } catch {
    return null;
  }
}

function notify() {
  for (const listener of listeners) listener();
}

export function getSession(): Session | null {
  if (cached === undefined) cached = read();
  return cached;
}

export function setSession(session: Session) {
  cached = session;
  try {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session));
  } catch {
    // Storage unavailable (private mode): the session lasts until reload.
  }
  notify();
}

/** Signs out. `reason`, if given, is shown once on the login page. */
export function clearSession(reason?: string) {
  if (getSession() === null) return;

  cached = null;
  notice = reason ?? null;
  leftOnPurpose = reason === undefined;
  try {
    localStorage.removeItem(SESSION_KEY);
  } catch {
    // Nothing to clear.
  }
  notify();
}

/** The reason for the last sign-out, if it had one. */
export function peekNotice() {
  return notice;
}

export function dismissNotice() {
  notice = null;
}

/**
 * Remembers the page a signed-out visitor was trying to reach, so signing in
 * can take them there instead of to the home page.
 */
export function rememberReturnTo(path: string) {
  if (leftOnPurpose) {
    leftOnPurpose = false;
    return;
  }

  try {
    if (path && path !== "/") sessionStorage.setItem(RETURN_TO_KEY, path);
  } catch {
    // Without storage they simply land on the home page.
  }
}

/** The remembered page, once. Only ever a path within this app. */
export function takeReturnTo() {
  try {
    const path = sessionStorage.getItem(RETURN_TO_KEY);
    sessionStorage.removeItem(RETURN_TO_KEY);
    if (path && path.startsWith("/") && !path.startsWith("//")) return path;
  } catch {
    // Fall through.
  }
  return "/";
}

function subscribe(listener: () => void) {
  listeners.add(listener);

  // Signing in or out in another tab updates this one too.
  const onStorage = (event: StorageEvent) => {
    if (event.key === SESSION_KEY) {
      cached = read();
      listener();
    }
  };
  window.addEventListener("storage", onStorage);

  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", onStorage);
  };
}

/**
 * The current session: a Session when signed in, null when signed out, and
 * undefined while that isn't known yet (during server rendering and hydration,
 * when browser storage can't be read).
 */
export function useSession(): Session | null | undefined {
  return useSyncExternalStore(subscribe, getSession, () => undefined);
}
