import { useCallback, useRef, useState } from "react";

type Size = { width: number; height: number };

/**
 * The size of an element, kept up to date as it changes: null until the
 * element exists and has been measured. Give the returned `ref` to the
 * element.
 */
export function useElementSize<T extends HTMLElement>() {
  const [size, setSize] = useState<Size | null>(null);
  const observer = useRef<ResizeObserver | null>(null);

  const ref = useCallback((element: T | null) => {
    observer.current?.disconnect();
    observer.current = null;
    if (!element) return;

    observer.current = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      setSize((was) => (was?.width === width && was.height === height ? was : { width, height }));
    });
    observer.current.observe(element);
  }, []);

  return [ref, size] as const;
}
