import { useEffect } from "react";

/** Sets the browser tab's title for as long as the calling page is shown. */
export function useTitle(title: string) {
  useEffect(() => {
    document.title = `${title} · coder`;
  }, [title]);
}
