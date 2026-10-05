import { useSyncExternalStore } from "react";

const noop = () => () => {};

/** Whether this is an Apple device, where the command key replaces Ctrl. */
export function useIsMac() {
  return useSyncExternalStore(
    noop,
    () => /Mac|iPhone|iPad/.test(navigator.platform),
    () => false,
  );
}

/** How the command-palette shortcut is written on this device. */
export function usePaletteShortcut() {
  return useIsMac() ? "⌘K" : "Ctrl K";
}
