"use client";

import { TriangleAlertIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { errorMessage, IDE_PORT, portUrl } from "@/lib/api";
import { Alert, AlertDescription } from "../ui/alert";
import { Button } from "../ui/button";
import { Spinner } from "../ui/spinner";

/** Where the editor opens: the user's home folder. */
export const IDE_PATH = "/?folder=/home/coder";

type State =
  { kind: "loading" } | { kind: "ready"; url: string } | { kind: "error"; message: string };

/**
 * The machine's editor (VS Code, served by code-server on the machine) in a
 * frame. The editor lives on its own origin, so it can't see this app's
 * session; a single-use link from the API is what lets the frame in.
 */
export function IdeView({ instanceId }: { instanceId: string }) {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;

    portUrl(instanceId, IDE_PORT, IDE_PATH).then(
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
  }, [instanceId, attempt]);

  if (state.kind === "loading") {
    return (
      <div
        className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground"
        role="status"
      >
        <Spinner />
        Opening the editor
      </div>
    );
  }

  if (state.kind === "error") {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 px-4">
        <Alert variant="destructive" className="max-w-md">
          <TriangleAlertIcon />
          <AlertDescription>{state.message}</AlertDescription>
        </Alert>
        <Button
          variant="outline"
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
      // The editor is always dark; this is what shows while it loads.
      className="min-h-0 w-full flex-1 border-0 bg-[#1f1f1f]"
    />
  );
}
