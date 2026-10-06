"use client";

import {
  ChevronRightIcon,
  FileWarningIcon,
  LockIcon,
  RotateCwIcon,
  SendIcon,
  ServerIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import {
  callPort,
  errorMessage,
  sendMessage,
  type Architecture,
  type PortAnswer,
  type PortCall,
} from "@/lib/api";
import { useInstances } from "@/lib/instances";
import {
  buildRequest,
  isJson,
  parseApiDoc,
  type ApiDoc,
  type ApiOperation,
  type ApiParameter,
} from "@/lib/openapi";
import { cn } from "@/lib/utils";
import { Alert, AlertDescription } from "../ui/alert";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Spinner } from "../ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { Blank, NotReady, PartPicker, partsOf, type Part } from "./shared";

/** Where every backend is required to serve its API's description. */
const RULE_PATH = "/openapi.json";

/**
 * Where to look for the description: where the rule says first, then where
 * common frameworks put theirs by default, so a backend that has one in the
 * wrong place is still shown, with a note.
 */
const DESCRIPTION_PATHS = [
  RULE_PATH,
  "/swagger/v1/swagger.json",
  "/openapi/v1.json",
  "/v3/api-docs",
  "/swagger.json",
  "/api-json",
  "/api/openapi.json",
];

/**
 * A chat's backend, as its own description of its API: every operation,
 * what it takes and returns, and a way to call it. With several backends in
 * the design there is a choice of which.
 */
export function BackendTab({
  chatId,
  architecture,
}: {
  chatId?: string;
  architecture: Architecture | null | undefined;
}) {
  const { instances } = useInstances();
  const parts = partsOf(architecture, instances, ["backend", "service"]);

  const [chosen, setChosen] = useState("");
  const part = parts.find((p) => p.node.id === chosen) ?? parts[0];

  if (!part) {
    return (
      <Blank icon={ServerIcon} title="No backend to show yet">
        {architecture?.status === "proposed"
          ? "The design is still a proposal. Once you approve it and its backend is built, its API appears here, to read and to try."
          : architecture
            ? "This design has no separate backend. An application that is one piece has its pages under Frontend."
            : "When the assistant builds a backend, its API appears here, to read and to try. Every backend is made to describe itself."}
      </Blank>
    );
  }

  return (
    <>
      <PartPicker parts={parts} value={part.node.id} onChange={setChosen} />
      <Api key={part.node.id} part={part} chatId={chatId} />
    </>
  );
}

type Found =
  | { kind: "loading" }
  | { kind: "ready"; doc: ApiDoc; path: string }
  | { kind: "missing" }
  | { kind: "error"; message: string };

function Api({ part, chatId }: { part: Part; chatId?: string }) {
  const { node, machine } = part;
  const ready = machine?.status === "running" && part.listening;

  const [found, setFound] = useState<Found>({ kind: "loading" });
  const [look, setLook] = useState(0);
  const [asking, setAsking] = useState(false);

  const machineId = machine?.id;
  useEffect(() => {
    if (!ready || !machineId) return;
    let cancelled = false;

    (async () => {
      try {
        for (const path of DESCRIPTION_PATHS) {
          const answer = await callPort(machineId, node.port, {
            path,
            headers: { Accept: "application/json" },
          });
          if (cancelled) return;

          const doc = answer.status === 200 && !answer.base64 ? parseApiDoc(answer.body) : null;
          if (doc) {
            setFound({ kind: "ready", doc, path });
            return;
          }
        }
        setFound({ kind: "missing" });
      } catch (err) {
        if (!cancelled) setFound({ kind: "error", message: errorMessage(err) });
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [ready, machineId, node.port, look]);

  if (!ready || !machine) return <NotReady part={part} icon={ServerIcon} />;

  function lookAgain() {
    setFound({ kind: "loading" });
    setLook((n) => n + 1);
  }

  async function askForIt() {
    if (!chatId) return;
    setAsking(true);
    try {
      await sendMessage(
        chatId,
        `The backend "${node.name}" on machine ${machine!.name} (port ${node.port}) does not serve a description of its API. Have its agent add an OpenAPI 3 document covering every route at ${RULE_PATH}, register the service again, and confirm it is served.`,
      );
      toast.success("Asked the assistant to add it.");
    } catch (err) {
      toast.error("Couldn't ask the assistant", { description: errorMessage(err) });
    } finally {
      setAsking(false);
    }
  }

  if (found.kind === "loading") {
    return (
      <div
        className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground"
        role="status"
      >
        <Spinner />
        Reading the API&apos;s description
      </div>
    );
  }

  if (found.kind === "error") {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 px-4">
        <Alert variant="destructive" className="max-w-md">
          <TriangleAlertIcon />
          <AlertDescription>{found.message}</AlertDescription>
        </Alert>
        <Button variant="outline" onClick={lookAgain}>
          Try again
        </Button>
      </div>
    );
  }

  if (found.kind === "missing") {
    return (
      <Blank
        icon={FileWarningIcon}
        title={`${node.name} doesn't describe its API`}
        action={
          <div className="flex flex-wrap justify-center gap-2">
            {chatId && (
              <Button size="sm" onClick={askForIt} disabled={asking}>
                {asking && <Spinner data-icon="inline-start" />}
                Ask the assistant to add it
              </Button>
            )}
            <Button size="sm" variant="outline" onClick={lookAgain}>
              Look again
            </Button>
          </div>
        }
      >
        It is running on{" "}
        <span className="font-mono text-foreground">
          {machine.name}:{node.port}
        </span>
        , but serves no OpenAPI document at{" "}
        <span className="font-mono text-foreground">{RULE_PATH}</span>. Every backend must: it is
        what this tab is drawn from.
      </Blank>
    );
  }

  return (
    <ApiView
      doc={found.doc}
      servedAt={found.path}
      machineId={machine.id}
      address={`${machine.name}:${node.port}`}
      port={node.port}
      onReload={lookAgain}
    />
  );
}

function ApiView({
  doc,
  servedAt,
  machineId,
  address,
  port,
  onReload,
}: {
  doc: ApiDoc;
  servedAt: string;
  machineId: string;
  address: string;
  port: number;
  onReload: () => void;
}) {
  // Sent with every request tried, for an API that wants credentials.
  const [authorization, setAuthorization] = useState("");

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="flex flex-col gap-2 border-b px-4 py-3">
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <h3 className="flex flex-wrap items-center gap-2 text-sm font-semibold">
              <span className="wrap-anywhere">{doc.title}</span>
              {doc.version && <Badge variant="outline">{doc.version}</Badge>}
            </h3>
            {doc.description && (
              <p className="mt-1 line-clamp-3 text-sm wrap-anywhere whitespace-pre-line text-muted-foreground">
                {doc.description}
              </p>
            )}
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Read the description again"
                onClick={onReload}
              >
                <RotateCwIcon />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Read it again</TooltipContent>
          </Tooltip>
        </div>

        <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span className="font-mono text-foreground">
            {address}
            {doc.basePath}
          </span>
          <span aria-hidden>·</span>
          <span>
            {doc.count} operation{doc.count === 1 ? "" : "s"}
          </span>
          <span aria-hidden>·</span>
          <span>
            {doc.kind}, from <span className="font-mono">{servedAt}</span>
          </span>
        </p>

        {servedAt !== RULE_PATH && (
          <p className="text-xs text-warning">
            Found at {servedAt}, not at {RULE_PATH} where every backend is meant to serve it.
          </p>
        )}

        {doc.secured && (
          <label className="mt-1 flex flex-col gap-1.5 text-xs font-medium">
            Authorization header, sent with every request you try
            <input
              value={authorization}
              onChange={(event) => setAuthorization(event.target.value)}
              placeholder="Bearer …"
              spellCheck={false}
              autoComplete="off"
              className="h-8 rounded-md border bg-card px-2.5 font-mono text-xs font-normal transition-colors outline-none focus-visible:border-foreground/50"
            />
          </label>
        )}
      </div>

      {doc.count === 0 && (
        <p className="p-4 text-sm text-muted-foreground">
          The description lists no operations yet.
        </p>
      )}

      {doc.groups.map((group) => (
        <section key={group.tag} aria-label={group.tag} className="border-b px-4 py-3">
          <h4 className="text-sm font-medium">{group.tag}</h4>
          {group.description && (
            <p className="mt-0.5 text-xs wrap-anywhere text-muted-foreground">
              {group.description}
            </p>
          )}
          <ul className="mt-2.5 flex flex-col gap-2">
            {group.operations.map((operation) => (
              <Operation
                key={operation.id}
                doc={doc}
                operation={operation}
                machineId={machineId}
                port={port}
                authorization={authorization}
              />
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

/** The colour a method is written in. Colour here says what a call does to data. */
const METHOD_TONE: Record<string, string> = {
  GET: "text-chart-2",
  POST: "text-success",
  PUT: "text-warning",
  PATCH: "text-warning",
  DELETE: "text-destructive",
};

const FIELD =
  "w-full rounded-md border bg-background px-2.5 font-mono text-xs transition-colors outline-none focus-visible:border-foreground/50";

function Operation({
  doc,
  operation,
  machineId,
  port,
  authorization,
}: {
  doc: ApiDoc;
  operation: ApiOperation;
  machineId: string;
  port: number;
  authorization: string;
}) {
  const [open, setOpen] = useState(false);
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(operation.parameters.map((p) => [`${p.in}:${p.name}`, p.example])),
  );
  const [body, setBody] = useState(operation.body?.example ?? "");
  const [sending, setSending] = useState(false);
  const [answer, setAnswer] = useState<PortAnswer | null>(null);
  const [problem, setProblem] = useState("");

  async function send() {
    const { path, headers, missing } = buildRequest(doc, operation, values);
    if (missing.length > 0) {
      setProblem(`Fill in ${missing.join(", ")} first.`);
      return;
    }

    const call: PortCall = {
      method: operation.method,
      path,
      headers: { Accept: "application/json, */*", ...headers },
    };
    if (authorization.trim()) call.headers!.Authorization = authorization.trim();
    if (operation.body && body.trim()) {
      call.body = body;
      call.headers!["Content-Type"] = operation.body.contentType;
    }

    setProblem("");
    setSending(true);
    try {
      setAnswer(await callPort(machineId, port, call));
    } catch (err) {
      setAnswer(null);
      setProblem(errorMessage(err));
    } finally {
      setSending(false);
    }
  }

  // Cookies can't be sent from here; everything else can be filled in.
  const fields = operation.parameters.filter((p) => p.in !== "cookie");

  return (
    <li className={cn("rounded-md border bg-card", open && "border-foreground/30")}>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <span
          className={cn(
            "w-12 shrink-0 font-mono text-[11px] font-semibold",
            METHOD_TONE[operation.method] ?? "text-muted-foreground",
          )}
        >
          {operation.method}
        </span>
        <span
          className={cn(
            "min-w-0 shrink truncate font-mono text-xs",
            operation.deprecated && "text-muted-foreground line-through",
          )}
        >
          {operation.path}
        </span>
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
          {operation.summary}
        </span>
        {operation.secured && (
          <LockIcon
            className="size-3.5 shrink-0 text-muted-foreground"
            aria-label="Needs credentials"
          />
        )}
        <ChevronRightIcon
          className={cn(
            "size-4 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
      </button>

      {open && (
        <div className="flex flex-col gap-4 border-t px-3 py-3">
          {(operation.description || operation.deprecated) && (
            <p className="text-xs wrap-anywhere whitespace-pre-line text-muted-foreground">
              {operation.deprecated && <span className="text-warning">Deprecated. </span>}
              {operation.description}
            </p>
          )}

          {fields.length > 0 && (
            <div className="flex flex-col gap-2.5">
              <p className="text-xs font-medium">Parameters</p>
              {fields.map((parameter) => (
                <Field
                  key={`${parameter.in}:${parameter.name}`}
                  parameter={parameter}
                  value={values[`${parameter.in}:${parameter.name}`] ?? ""}
                  onChange={(value) =>
                    setValues((all) => ({ ...all, [`${parameter.in}:${parameter.name}`]: value }))
                  }
                />
              ))}
            </div>
          )}

          {operation.body && (
            <label className="flex flex-col gap-1.5">
              <span className="flex flex-wrap items-baseline gap-x-2 text-xs font-medium">
                Body
                <span className="font-mono font-normal text-muted-foreground">
                  {operation.body.contentType}
                </span>
                {operation.body.required && (
                  <span className="font-normal text-muted-foreground">required</span>
                )}
              </span>
              <textarea
                value={body}
                onChange={(event) => setBody(event.target.value)}
                rows={Math.min(14, Math.max(3, body.split("\n").length))}
                spellCheck={false}
                className={cn(FIELD, "resize-y py-2 leading-relaxed")}
              />
            </label>
          )}

          <div className="flex flex-wrap items-center gap-3">
            <Button size="sm" onClick={send} disabled={sending}>
              {sending ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SendIcon data-icon="inline-start" />
              )}
              Send
            </Button>
            {problem && (
              <p className="text-xs text-destructive" role="alert">
                {problem}
              </p>
            )}
          </div>

          {answer && <Answer answer={answer} />}

          {operation.responses.length > 0 && (
            <div className="flex flex-col gap-1.5">
              <p className="text-xs font-medium">What it can answer</p>
              <ul className="flex flex-col gap-1.5">
                {operation.responses.map((response) => (
                  <li key={response.status} className="text-xs">
                    <details className="group/response">
                      <summary
                        className={cn(
                          "flex list-none items-baseline gap-2 rounded-sm outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 [&::-webkit-details-marker]:hidden",
                          response.example && "cursor-pointer",
                        )}
                      >
                        <span
                          className={cn(
                            "w-12 shrink-0 font-mono font-semibold",
                            statusTone(response.status),
                          )}
                        >
                          {response.status}
                        </span>
                        <span className="min-w-0 flex-1 wrap-anywhere text-muted-foreground">
                          {response.description || "No description"}
                        </span>
                        {response.example && (
                          <ChevronRightIcon className="size-3.5 shrink-0 self-center text-muted-foreground transition-transform group-open/response:rotate-90" />
                        )}
                      </summary>
                      {response.example && <Code className="mt-1.5 ml-14">{response.example}</Code>}
                    </details>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </li>
  );
}

function Field({
  parameter,
  value,
  onChange,
}: {
  parameter: ApiParameter;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="flex flex-wrap items-baseline gap-x-2 text-xs">
        <span className="font-mono font-medium">{parameter.name}</span>
        <span className="text-muted-foreground">
          {[parameter.in, parameter.type, parameter.required ? "required" : ""]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </span>
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        spellCheck={false}
        autoComplete="off"
        aria-required={parameter.required}
        className={cn(FIELD, "h-8")}
      />
      {parameter.description && (
        <span className="text-xs wrap-anywhere text-muted-foreground">{parameter.description}</span>
      )}
    </label>
  );
}

/** The colour of a status: green for done, amber for refused, red for broken. */
function statusTone(status: string | number) {
  const code = Number(status);
  if (code >= 200 && code < 300) return "text-success";
  if (code >= 400 && code < 500) return "text-warning";
  if (code >= 500) return "text-destructive";
  return "text-muted-foreground";
}

/** What came back from a request that was tried. */
function Answer({ answer }: { answer: PortAnswer }) {
  const contentType = answer.headers["Content-Type"] ?? "";

  // JSON is laid out to be read; anything else is shown as it came.
  let text = answer.body;
  if (!answer.base64 && (isJson(contentType) || /^\s*[[{]/.test(text))) {
    try {
      text = JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      // Not JSON after all.
    }
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border bg-background p-3" role="status">
      <p className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-xs">
        <span className={cn("font-mono text-sm font-semibold", statusTone(answer.status))}>
          {answer.status}
        </span>
        <span className="text-muted-foreground">{answer.duration_ms} ms</span>
        {contentType && <span className="font-mono text-muted-foreground">{contentType}</span>}
      </p>

      {answer.base64 ? (
        <p className="text-xs text-muted-foreground">
          The answer is not text ({Math.floor((answer.body.length * 3) / 4).toLocaleString()}{" "}
          bytes).
        </p>
      ) : text ? (
        <Code>{text}</Code>
      ) : (
        <p className="text-xs text-muted-foreground">No body.</p>
      )}
      {answer.truncated && (
        <p className="text-xs text-warning">Only the first 2 MB of the answer is shown.</p>
      )}

      <details className="group/headers text-xs">
        <summary className="flex cursor-pointer list-none items-center gap-1 text-muted-foreground outline-none hover:text-foreground focus-visible:text-foreground [&::-webkit-details-marker]:hidden">
          <ChevronRightIcon className="size-3.5 transition-transform group-open/headers:rotate-90" />
          Headers
        </summary>
        <dl className="mt-1.5 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 font-mono">
          {Object.entries(answer.headers)
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([name, value]) => (
              <div key={name} className="contents">
                <dt className="text-muted-foreground">{name}</dt>
                <dd className="wrap-anywhere">{value}</dd>
              </div>
            ))}
        </dl>
      </details>
    </div>
  );
}

function Code({ className, children }: { className?: string; children: string }) {
  return (
    <pre
      className={cn(
        "max-h-80 overflow-auto rounded-md border bg-muted/40 p-2.5 font-mono text-xs leading-relaxed",
        className,
      )}
    >
      {children}
    </pre>
  );
}
