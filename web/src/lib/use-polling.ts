import { useEffect } from "react";

/**
 * Calls `poll` once immediately, then every `intervalMs` while the tab is
 * visible, and again as soon as a hidden tab is shown. `poll` should be
 * stable (wrap it in useCallback); a new identity restarts the cycle.
 */
export function usePolling(poll: () => void, intervalMs: number) {
  useEffect(() => {
    poll();

    const pollIfVisible = () => {
      if (document.visibilityState === "visible") poll();
    };
    const timer = setInterval(pollIfVisible, intervalMs);
    document.addEventListener("visibilitychange", pollIfVisible);

    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", pollIfVisible);
    };
  }, [poll, intervalMs]);
}
