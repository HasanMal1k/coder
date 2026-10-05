import { useCallback, useSyncExternalStore } from "react";

/**
 * Whether the window matches a CSS media query, kept up to date as it is
 * resized. False on the server, where there is no window to measure.
 */
export function useMediaQuery(query: string) {
  const subscribe = useCallback(
    (onChange: () => void) => {
      const mql = window.matchMedia(query);
      mql.addEventListener("change", onChange);
      return () => mql.removeEventListener("change", onChange);
    },
    [query],
  );

  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(query).matches,
    () => false,
  );
}
