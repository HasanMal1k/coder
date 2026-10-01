import { clearSession } from "./session";

export const API_URL = (
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"
).replace(/\/+$/, "");

export type User = {
  id: string;
  email: string;
  display_name: string;
  organization: string;
  subscription: string;
};

export type InstanceStatus =
  | "creating"
  | "stopped"
  | "starting"
  | "running"
  | "stopping"
  | "deleting"
  | "error";

export type Instance = {
  id: string;
  name: string;
  status: InstanceStatus;
  cpus: number;
  memory_mb: number;
  disk_gb: number;
  created_at: string;
  updated_at: string;
};

export type AuthResult = { token: string; user: User };

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

async function request<T>(
  method: string,
  path: string,
  options: { token?: string; body?: unknown } = {},
): Promise<T> {
  const headers: Record<string, string> = {};
  if (options.token) headers["Authorization"] = `Bearer ${options.token}`;
  if (options.body !== undefined) headers["Content-Type"] = "application/json";

  let response: Response;
  try {
    response = await fetch(API_URL + path, {
      method,
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
  } catch {
    throw new ApiError(0, "Can't reach the server. Check that it is running.");
  }

  if (response.ok) {
    if (response.status === 204) return undefined as T;
    return (await response.json()) as T;
  }

  // A 401 on an authenticated call means the token expired: sign out, so the
  // app sends the user back to the login page.
  if (response.status === 401 && options.token) {
    clearSession("Your session expired. Sign in again to continue.");
  }

  let message = `Request failed (${response.status})`;
  try {
    const body = (await response.json()) as { error?: string };
    if (body.error) message = body.error;
  } catch {
    // Not JSON; keep the generic message.
  }
  throw new ApiError(response.status, message);
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

export async function listInstances(token: string) {
  const result = await request<{ instances: Instance[] }>("GET", "/instances", { token });
  return result.instances;
}

export function getInstance(token: string, id: string) {
  return request<Instance>("GET", `/instances/${id}`, { token });
}

export function createInstance(token: string, name: string) {
  return request<Instance>("POST", "/instances", { token, body: { name } });
}

export function startInstance(token: string, id: string) {
  return request<Instance>("POST", `/instances/${id}/start`, { token });
}

export function stopInstance(token: string, id: string) {
  return request<Instance>("POST", `/instances/${id}/stop`, { token });
}

export function deleteInstance(token: string, id: string) {
  return request<void>("DELETE", `/instances/${id}`, { token });
}

/** The port the editor listens on inside every machine. */
export const IDE_PORT = 13337;

/**
 * Returns a link that opens one of the machine's ports in the browser,
 * landing on `path`. The link works once and expires within a minute, so ask
 * for a new one each time rather than keeping it.
 */
export async function portUrl(token: string, id: string, port: number, path = "/") {
  const result = await request<{ url: string }>("POST", `/instances/${id}/ports/${port}/url`, {
    token,
    body: { path },
  });
  return result.url;
}

/** The WebSocket URL for a terminal on the given instance. */
export function terminalUrl(id: string) {
  return `${API_URL.replace(/^http/, "ws")}/instances/${id}/terminal`;
}

/** The message to show a user for any thrown value. */
export function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : "Something went wrong.";
}
