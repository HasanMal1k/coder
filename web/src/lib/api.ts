import { clearSession, getSession } from "./session";

export const API_URL = (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080").replace(
  /\/+$/,
  "",
);

export type User = {
  id: string;
  email: string;
  display_name: string;
  organization: string;
  subscription: string;
};

export type InstanceStatus =
  "creating" | "stopped" | "starting" | "running" | "stopping" | "deleting" | "error";

export type Instance = {
  id: string;
  name: string;
  status: InstanceStatus;
  cpus: number;
  memory_mb: number;
  disk_gb: number;
  /** The toolchain the machine came with; see `STACKS` in sizes.ts. */
  stack: StackName;
  created_at: string;
  updated_at: string;
};

export type StackName = "base" | "go" | "cpp" | "dotnet" | "data";

export type AuthResult = { token: string; user: User };

export const SESSION_EXPIRED = "Your session expired. Sign in again to continue.";

/** An error response from the API. `status` is 0 when the server couldn't be reached. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/**
 * The signed-in user's access token, read at the moment of use. Tokens are
 * renewed in the background, so callers must not hold on to one.
 */
export function currentToken() {
  const session = getSession();
  if (!session) throw new ApiError(401, SESSION_EXPIRED);
  return session.token;
}

async function request<T>(
  method: string,
  path: string,
  options: { auth?: boolean; body?: unknown } = {},
): Promise<T> {
  const headers: Record<string, string> = {};
  if (options.auth) headers["Authorization"] = `Bearer ${currentToken()}`;
  if (options.body !== undefined) headers["Content-Type"] = "application/json";

  let response: Response;
  try {
    response = await fetch(API_URL + path, {
      method,
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
  } catch {
    throw new ApiError(0, "Can't reach the server. Check your connection and that it is running.");
  }

  if (response.ok) {
    // 202 and 204 are answers with nothing in them.
    if (response.status === 202 || response.status === 204) return undefined as T;
    return (await response.json()) as T;
  }

  throw await failure(response, options.auth ?? false);
}

/** Turns an error response into the ApiError to throw for it. */
async function failure(response: Response, auth: boolean) {
  // A 401 on an authenticated call means the token is no longer accepted:
  // sign out, so the app sends the user back to the login page.
  if (response.status === 401 && auth) {
    clearSession(SESSION_EXPIRED);
  }

  let message = `Request failed (${response.status})`;
  try {
    const body = (await response.json()) as { error?: string };
    if (body.error) message = body.error;
  } catch {
    // Not JSON; keep the generic message.
  }
  return new ApiError(response.status, message);
}

export function login(email: string, password: string) {
  return request<AuthResult>("POST", "/login", { body: { email, password } });
}

export function register(input: {
  email: string;
  password: string;
  display_name: string;
  organization: string;
}) {
  return request<AuthResult>("POST", "/register", { body: input });
}

/** Trades the current, still-valid token for a fresh one. */
export function renewToken() {
  return request<AuthResult>("POST", "/token/renew", { auth: true });
}

export async function listInstances() {
  const result = await request<{ instances: Instance[] }>("GET", "/instances", { auth: true });
  return result.instances;
}

/** How powerful a machine is; see `POWERS` in sizes.ts. */
export type PowerName = "small" | "medium" | "large";

/** Creates a machine. Leave out `size`, `diskGb` or `stack` for the server's default. */
export function createInstance(name: string, size?: PowerName, diskGb?: number, stack?: StackName) {
  return request<Instance>("POST", "/instances", {
    auth: true,
    body: { name, size, disk_gb: diskGb, stack },
  });
}

export function startInstance(id: string) {
  return request<Instance>("POST", `/instances/${id}/start`, { auth: true });
}

export function stopInstance(id: string) {
  return request<Instance>("POST", `/instances/${id}/stop`, { auth: true });
}

export function deleteInstance(id: string) {
  return request<void>("DELETE", `/instances/${id}`, { auth: true });
}

/** The port the editor listens on inside every machine. */
export const IDE_PORT = 13337;

/**
 * Returns a link that opens one of the machine's ports in the browser,
 * landing on `path`. The link works once and expires within a minute, so ask
 * for a new one each time rather than keeping it.
 */
export async function portUrl(id: string, port: number, path = "/") {
  const result = await request<{ url: string }>("POST", `/instances/${id}/ports/${port}/url`, {
    auth: true,
    body: { path },
  });
  return result.url;
}

/** An HTTP request for something listening inside a machine. */
export type PortCall = {
  /** Defaults to GET. */
  method?: string;
  /** The path to ask for, with any query. Defaults to "/". */
  path?: string;
  headers?: Record<string, string>;
  body?: string;
};

/** What the machine answered. */
export type PortAnswer = {
  status: number;
  headers: Record<string, string>;
  body: string;
  /** `body` is base64: what came back was not text. */
  base64?: boolean;
  /** `body` is only the start of what came back. */
  truncated?: boolean;
  duration_ms: number;
};

/**
 * Makes an HTTP request to a port inside a machine, through the server, and
 * returns the answer as data. The machine's own status, a 404 or a 500
 * included, is a successful call; this only throws when the request could
 * not be made at all (the machine is stopped, nothing listens on the port).
 */
export function callPort(id: string, port: number, call: PortCall = {}) {
  return request<PortAnswer>("POST", `/instances/${id}/ports/${port}/request`, {
    auth: true,
    body: call,
  });
}

/** The WebSocket URL for a terminal on the given instance. */
export function terminalUrl(id: string) {
  return `${API_URL.replace(/^http/, "ws")}/instances/${id}/terminal`;
}

/** The WebSocket URL of the connection that tells the server this tab is open. */
export function presenceUrl() {
  return `${API_URL.replace(/^http/, "ws")}/presence`;
}

export type ChatStatus = "idle" | "running" | "waiting";

export type Chat = {
  id: string;
  title: string;
  /** running: the assistant is working. waiting: it asked something. */
  status: ChatStatus;
  created_at: string;
  updated_at: string;
};

/** The assistant asking for something to be done. */
export type ToolCall = {
  id: string;
  type: string;
  /** `arguments` is a JSON object, as text. */
  function: { name: string; arguments: string };
};

export type ToolResult = {
  tool_call_id: string;
  name: string;
  content: string;
  failed?: boolean;
  /** The machine the result is about, when there is one. */
  machine?: {
    id: string;
    name: string;
    status: string;
    cpus: number;
    memory_mb: number;
    disk_gb: number;
  };
};

type EventBase = { seq: number; created_at: string };

/** A background agent has finished the task it was given. */
export type AgentNews = {
  agent_id: string;
  agent: string;
  /** The call that started the agent's task. */
  tool_call_id: string;
  machine: { id: string; name: string };
  /** Its report, or why it failed. */
  content: string;
  failed?: boolean;
  /** The user stopped it. */
  stopped?: boolean;
};

/** One agent asked another something. */
export type AgentExchange = {
  from: { machine: string; agent: string };
  to: { machine: string; agent: string };
  question: string;
  /** Empty when the other agent was busy and the question was left for it. */
  answer?: string;
  queued?: boolean;
  failed?: boolean;
};

export type NodeKind =
  "frontend" | "backend" | "service" | "database" | "cache" | "queue" | "ci" | "external";

/** One component of a design. */
export type DesignNode = {
  id: string;
  kind: NodeKind;
  name: string;
  /** What it is made with: "Next.js", "PostgreSQL". */
  tech: string;
  why?: string;
  /** The machine it runs on; absent for an external component. */
  machine?: string;
  stack?: StackName;
  size?: PowerName;
  disk_gb?: number;
  port?: number;
};

/** One component using another. */
export type DesignEdge = {
  from: string;
  to: string;
  label?: string;
  /** A path on `from`'s address that the browser's requests reach `to` by. */
  path?: string;
  /** The variable `from`'s programs read `to`'s address from. */
  env?: string;
};

/** The architecture of what a chat is building. */
export type Design = {
  summary: string;
  structure: "single-app" | "frontend-and-backend" | "microservices";
  repository: "one-repository" | "repository-per-component";
  reasons: string[];
  nodes: DesignNode[];
  edges: DesignEdge[];
};

/** A chat's design, and how what it describes stands right now. */
export type Architecture = {
  /** proposed: waiting for the user to agree. approved: what is being built. */
  status: "proposed" | "approved";
  design: Design;
  nodes: {
    id: string;
    /** The machine the component is on, once it exists. */
    machine?: { id: string; name: string; status: InstanceStatus };
    /** Whether something answers on its port; absent when that can't be known. */
    listening?: boolean;
  }[];
  updated_at: string;
};

/** The chat's design, or null if it has none. */
export async function getArchitecture(chatId: string) {
  try {
    return await request<Architecture>("GET", `/chats/${chatId}/architecture`, { auth: true });
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

/**
 * Agrees to the design the assistant proposed, which sets it building. Pass
 * `design` to approve it as changed.
 */
export function approveArchitecture(chatId: string, design?: Design) {
  return request<void>("POST", `/chats/${chatId}/architecture/approve`, {
    auth: true,
    body: design ? { design } : {},
  });
}

export type AgentStatus = "idle" | "working" | "failed" | "stopped";

/** A coding agent: a named worker inside one of the user's machines. */
export type Agent = {
  id: string;
  name: string;
  chat_id: string;
  machine: { id: string; name: string };
  status: AgentStatus;
  /** What it is working on, or last worked on. */
  task: string;
  /** Whether that task was started without the assistant waiting for it. */
  background: boolean;
  /** What it is doing right now, while working. */
  activity: string;
  /** What came of its last task: its report, or why it failed. */
  report: string;
  started_at: string | null;
  finished_at: string | null;
  updated_at: string;
};

/** The user's agents from every chat, those working first. */
export async function listAgents() {
  const result = await request<{ agents: Agent[] }>("GET", "/agents", { auth: true });
  return result.agents;
}

/** Stops what an agent is doing. What it has done so far is kept. */
export function stopAgent(id: string) {
  return request<void>("POST", `/agents/${id}/stop`, { auth: true });
}

/** One entry in a chat's history. */
export type ChatEvent = EventBase &
  (
    | { kind: "user"; payload: { text: string } }
    | { kind: "assistant"; payload: { text: string; tool_calls?: ToolCall[] } }
    | { kind: "tool"; payload: ToolResult }
    | { kind: "activity"; payload: { tool_call_id: string; machine: string; text: string } }
    | { kind: "agent"; payload: AgentNews }
    | { kind: "message"; payload: AgentExchange }
    | { kind: "error"; payload: { message: string; stopped?: boolean } }
  );

/** Whether this server has the assistant set up, and with which model. */
export function assistantInfo() {
  return request<{ enabled: boolean; model?: string }>("GET", "/assistant", { auth: true });
}

export async function listChats() {
  const result = await request<{ chats: Chat[] }>("GET", "/chats", { auth: true });
  return result.chats;
}

export function createChat() {
  return request<Chat>("POST", "/chats", { auth: true });
}

export function deleteChat(id: string) {
  return request<void>("DELETE", `/chats/${id}`, { auth: true });
}

/**
 * Sends a message to a chat. It resolves once the message is accepted; the
 * assistant's reply arrives afterwards, through `streamChat`.
 */
export function sendMessage(id: string, text: string) {
  return request<void>("POST", `/chats/${id}/messages`, { auth: true, body: { text } });
}

/** Stops the assistant, or withdraws the question it is waiting on. */
export function cancelChat(id: string) {
  return request<void>("POST", `/chats/${id}/cancel`, { auth: true });
}

export type ChatStreamHandlers = {
  /** A new entry in the history. */
  event: (event: ChatEvent) => void;
  /** The chat's status or title changed. */
  chat: (chat: Chat) => void;
  /**
   * What the assistant has written so far of the message it is composing.
   * Empty once that message is finished (it then arrives as an event).
   */
  draft: (text: string) => void;
  deleted: () => void;
};

/**
 * Follows a chat: every entry after sequence number `after`, then each new
 * one as it happens. Resolves when the server ends the stream and rejects if
 * it can't be opened or breaks; either way the caller reconnects from the
 * last entry it saw, and misses nothing.
 */
export async function streamChat(
  id: string,
  after: number,
  handlers: ChatStreamHandlers,
  signal: AbortSignal,
) {
  let response: Response;
  try {
    response = await fetch(`${API_URL}/chats/${id}/stream?after=${after}`, {
      headers: { Authorization: `Bearer ${currentToken()}`, Accept: "text/event-stream" },
      signal,
    });
  } catch (err) {
    if (signal.aborted || err instanceof ApiError) throw err;
    throw new ApiError(0, "Can't reach the server.");
  }
  if (!response.ok || !response.body) throw await failure(response, true);

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const { done, value } = await reader.read();
    if (done) return;
    buffer += decoder.decode(value, { stream: true });

    // Events are separated by a blank line.
    let end: number;
    while ((end = buffer.indexOf("\n\n")) >= 0) {
      const block = buffer.slice(0, end);
      buffer = buffer.slice(end + 2);

      let name = "message";
      let data = "";
      for (const line of block.split("\n")) {
        if (line.startsWith("event:")) name = line.slice(6).trim();
        else if (line.startsWith("data:")) data += line.slice(5).trim();
      }
      // A block with no data is a keep-alive comment.
      if (!data) continue;

      if (name === "event") handlers.event(JSON.parse(data) as ChatEvent);
      else if (name === "chat") handlers.chat(JSON.parse(data) as Chat);
      else if (name === "draft") handlers.draft((JSON.parse(data) as { text: string }).text);
      else if (name === "deleted") handlers.deleted();
    }
  }
}

/** The message to show a user for any thrown value. */
export function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : "Something went wrong.";
}
