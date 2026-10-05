"use client";

import { AppWindowIcon, ExternalLinkIcon, RotateCwIcon, TriangleAlertIcon } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { errorMessage, portUrl, type Architecture } from "@/lib/api";
import { useInstances } from "@/lib/instances";
import { Alert, AlertDescription } from "../ui/alert";
import { Button } from "../ui/button";
import { Spinner } from "../ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { Blank, NotReady, PartPicker, partsOf, type Part } from "./shared";

/**
 * The page the chat's frontend serves, live, in a frame. With several
 * frontends in the design there is a choice of which.
 */
export function FrontendTab({ architecture }: { architecture: Architecture | null | undefined }) {
  const { instances } = useInstances();
  const parts = partsOf(architecture, instances, ["frontend"]);

  const [chosen, setChosen] = useState("");
  const part = parts.find((p) => p.node.id === chosen) ?? parts[0];

  if (!part) {
    return (
      <Blank icon={AppWindowIcon} title="No frontend to show yet">
        {architecture?.status === "proposed"
          ? "The design is still a proposal. Once you approve it and its frontend is built, the page appears here, live."
          : architecture
            ? "This design has no frontend that serves a page."
            : "When the assistant builds something with a web page, the page appears here, live."}
      </Blank>
    );
  }

  return (
    <>
      <PartPicker parts={parts} value={part.node.id} onChange={setChosen} />
      {/* Keyed by component, so choosing another starts from its own front page. */}
      <Preview key={part.node.id} part={part} />
    </>
  );
}

type Frame =
  { kind: "loading" } | { kind: "ready"; url: string } | { kind: "error"; message: string };

function Preview({ part }: { part: Part }) {
  const { node, machine } = part;
  const ready = machine?.status === "running" && part.listening;

  // Where in the site the frame was last sent, and what is typed but not
  // yet gone to. The frame is on another origin, so where the user has
  // clicked to since can't be read back.
  const [path, setPath] = useState("/");
  const [typed, setTyped] = useState("/");
  const [load, setLoad] = useState(0);
  const [frame, setFrame] = useState<Frame>({ kind: "loading" });

  const machineId = machine?.id;
  useEffect(() => {
    if (!ready || !machineId) return;
    let cancelled = false;

    // The page is on the machine's own origin, which this app's session
    // doesn't reach; a single-use link from the server is what lets the
    // frame in.
    portUrl(machineId, node.port, path).then(
      (url) => {
        if (!cancelled) setFrame({ kind: "ready", url });
      },
      (err) => {
        if (!cancelled) setFrame({ kind: "error", message: errorMessage(err) });
      },
    );

    return () => {
      cancelled = true;
    };
  }, [ready, machineId, node.port, path, load]);

  if (!ready || !machine) return <NotReady part={part} icon={AppWindowIcon} />;

  function reload() {
    setFrame({ kind: "loading" });
    setLoad((n) => n + 1);
  }

  function go(event: FormEvent) {
    event.preventDefault();
    const next = typed.startsWith("/") ? typed : `/${typed}`;
    setTyped(next);
    setFrame({ kind: "loading" });
    setPath(next);
    setLoad((n) => n + 1);
  }

  async function openTab() {
    // Opened during the click, pointed at the page once its link arrives:
    // a tab opened after waiting would be blocked as a pop-up.
    const tab = window.open("", "_blank");
    try {
      const url = await portUrl(machine!.id, node.port, path);
      if (tab) {
        tab.opener = null;
        tab.location.href = url;
      } else {
        window.location.href = url;
      }
    } catch (err) {
      tab?.close();
      toast.error(`Couldn't open ${node.name}`, { description: errorMessage(err) });
    }
  }

  return (
    <>
      <form onSubmit={go} className="flex shrink-0 items-center gap-1.5 border-b px-3 py-2">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Reload the page"
              onClick={reload}
            >
              <RotateCwIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Reload</TooltipContent>
        </Tooltip>

        {/* An address bar: the machine and port are fixed, the path is typed. */}
        <label className="flex h-8 min-w-0 flex-1 items-center rounded-md border bg-card px-2.5 font-mono text-xs transition-colors focus-within:border-foreground/50">
          <span className="shrink-0 text-muted-foreground">
            {machine.name}:{node.port}
          </span>
          <input
            value={typed}
            onChange={(event) => setTyped(event.target.value)}
            aria-label="Path"
            spellCheck={false}
            autoComplete="off"
            className="min-w-0 flex-1 bg-transparent outline-none"
          />
        </label>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Open in a new tab"
              onClick={openTab}
            >
              <ExternalLinkIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Open in a new tab</TooltipContent>
        </Tooltip>
      </form>

      {frame.kind === "loading" && (
        <div
          className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground"
          role="status"
        >
          <Spinner />
          Opening {node.name}
        </div>
      )}

      {frame.kind === "error" && (
        <div className="flex flex-1 flex-col items-center justify-center gap-4 px-4">
          <Alert variant="destructive" className="max-w-md">
            <TriangleAlertIcon />
            <AlertDescription>{frame.message}</AlertDescription>
          </Alert>
          <Button variant="outline" onClick={reload}>
            Try again
          </Button>
        </div>
      )}

      {frame.kind === "ready" && (
        <iframe
          // A fresh link each time, so the frame is made anew for each.
          key={frame.url}
          src={frame.url}
          title={`${node.name}, live`}
          // A page with no background of its own is drawn on white, as a
          // browser would draw it.
          className="min-h-0 w-full flex-1 border-0 bg-white"
        />
      )}
    </>
  );
}
