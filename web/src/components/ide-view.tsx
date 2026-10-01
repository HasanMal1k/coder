"use client";

import { useEffect, useState } from "react";
import { errorMessage, IDE_PORT, portUrl } from "@/lib/api";
import { Button, ErrorNote, PageSpinner } from "./ui";

/** Where the editor opens: the user's home folder. */
export const IDE_PATH = "/?folder=/home/coder";

type State =
  | { kind: "loading" }
  | { kind: "ready"; url: string }
  | { kind: "error"; message: string };

/**
 * The machine's editor (VS Code, served by code-server on the machine) in a
 * frame. The editor lives on its own origin, so it can't see this app's
 * session; a single-use link from the API is what lets the frame in.
 */
export function IdeView({ instanceId, token }: { instanceId: string; token: string }) {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;

    portUrl(token, instanceId, IDE_PORT, IDE_PATH).then(
      (url) => {
        if (!cancelled) setState({ kind: "ready", url });
      },
      (err) => {
        if (!cancelled) setState({ kind: "error", message: errorMessage(err) });
      },
    );

    return () => {
      cancelled = true;
    };
  }, [instanceId, token, attempt]);

  if (state.kind === "loading") return <PageSpinner label="Opening the editor" />;

  if (state.kind === "error") {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 px-4">
        <ErrorNote>{state.message}</ErrorNote>
        <Button
          onClick={() => {
            setState({ kind: "loading" });
            setAttempt((n) => n + 1);
          }}
        >
          Try again
        </Button>
      </div>
    );
  }

  return (
    <iframe
      src={state.url}
      title="Editor"
      allow="clipboard-read; clipboard-write"
      className="min-h-0 w-full flex-1 border-0 bg-term"
    />
  );
}
