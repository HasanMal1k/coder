import { useCallback, useState } from "react";
import { getArchitecture, type Architecture } from "./api";
import { usePolling } from "./use-polling";

/** How often the design and the state of what it describes are looked at. */
const REFRESH_MS = 3000;

/**
 * A chat's design, kept up to date: `undefined` until first loaded, `null`
 * if the chat has none. Remount (change `key`) to follow another chat. Pass
 * a longer interval where being a few seconds behind does not matter.
 */
export function useArchitecture(chatId: string, intervalMs = REFRESH_MS) {
  const [architecture, setArchitecture] = useState<Architecture | null | undefined>(undefined);

  const refresh = useCallback(async () => {
    try {
      setArchitecture(await getArchitecture(chatId));
    } catch {
      // Left as it was; the next look will try again.
    }
  }, [chatId]);

  usePolling(refresh, intervalMs);

  return { architecture, refresh };
}
