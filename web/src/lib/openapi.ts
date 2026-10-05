/**
 * Reading an API's description (OpenAPI 3, or Swagger 2) into what the
 * Backend tab draws: its operations, what each takes, and what each returns.
 * Nothing here talks to a server.
 */

type Obj = Record<string, unknown>;

/** A value a request needs: part of the path, the query, or a header. */
export type ApiParameter = {
  name: string;
  in: "path" | "query" | "header" | "cookie";
  required: boolean;
  /** Its type in words: "string", "integer", "array of string". */
  type: string;
  description: string;
  /** Something plausible to send, as text. Empty if there is nothing to suggest. */
  example: string;
};

export type ApiBody = {
  contentType: string;
  required: boolean;
  description: string;
  /** A body of the right shape, as text ready to send. */
  example: string;
};

export type ApiResponse = {
  /** "200", "404", or "default". */
  status: string;
  description: string;
  contentType: string;
  /** What comes back, as text. Empty when the description doesn't say. */
  example: string;
};

export type ApiOperation = {
  /** Unique within the description: the method and the path. */
  id: string;
  /** Upper case. */
  method: string;
  /** As written in the description, with {placeholders}. */
  path: string;
  summary: string;
  description: string;
  deprecated: boolean;
  /** It asks for credentials. */
  secured: boolean;
  parameters: ApiParameter[];
  body: ApiBody | null;
  responses: ApiResponse[];
};

export type ApiGroup = { tag: string; description: string; operations: ApiOperation[] };

export type ApiDoc = {
  title: string;
  version: string;
  description: string;
  /** Which kind of description it is: "OpenAPI 3.0.3". */
  kind: string;
  /** What every path is under, without a trailing slash: "" or "/v1". */
  basePath: string;
  /** Some operation asks for credentials. */
  secured: boolean;
  groups: ApiGroup[];
  count: number;
};

const METHODS = ["get", "post", "put", "patch", "delete", "head", "options"];

/** How deep an example goes before giving up on a schema that keeps nesting. */
const MAX_DEPTH = 8;

function isObj(value: unknown): value is Obj {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown) {
  return typeof value === "string" ? value : "";
}

function list(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

/** What a "$ref" inside the document points to, or nothing if it leads nowhere. */
function lookup(root: Obj, ref: string): unknown {
  if (!ref.startsWith("#/")) return undefined;

  let at: unknown = root;
  for (const part of ref.slice(2).split("/")) {
    if (!isObj(at)) return undefined;
    at = at[decodeURIComponent(part).replace(/~1/g, "/").replace(/~0/g, "~")];
  }
  return at;
}

/** A node with any chain of references followed to what they stand for. */
function deref(root: Obj, node: unknown): Obj {
  const seen = new Set<string>();
  let at = node;

  while (isObj(at) && typeof at.$ref === "string") {
    if (seen.has(at.$ref)) return {};
    seen.add(at.$ref);
    at = lookup(root, at.$ref);
  }
  return isObj(at) ? at : {};
}

/** The one type a schema is, leaving "null" aside. */
function schemaType(schema: Obj) {
  const type = Array.isArray(schema.type)
    ? schema.type.find((t) => t !== "null")
    : (schema.type as unknown);
  if (typeof type === "string") return type;
  if (isObj(schema.properties) || schema.additionalProperties) return "object";
  if (schema.items) return "array";
  return "";
}

const STRING_EXAMPLES: Record<string, string> = {
  "date-time": "2026-01-01T12:00:00Z",
  date: "2026-01-01",
  time: "12:00:00",
  uuid: "3fa85f64-5717-4562-b3fc-2c963f66afa6",
  email: "user@example.com",
  uri: "https://example.com",
  url: "https://example.com",
  hostname: "example.com",
  ipv4: "192.0.2.1",
};

/**
 * A value of the shape a schema describes: its own example where it gives
 * one, and otherwise something plain of the right type. `path` is the
 * references being followed, so a schema that contains itself ends.
 */
function exampleOf(root: Obj, node: unknown, path: string[] = []): unknown {
  if (path.length > MAX_DEPTH) return null;

  let schema = isObj(node) ? node : {};
  if (typeof schema.$ref === "string") {
    if (path.includes(schema.$ref)) return null;
    path = [...path, schema.$ref];
    schema = deref(root, schema);
  } else {
    path = [...path, ""];
  }

  if (schema.example !== undefined) return schema.example;
  if (Array.isArray(schema.examples) && schema.examples.length > 0) return schema.examples[0];
  if (schema.default !== undefined) return schema.default;
  if (schema.const !== undefined) return schema.const;
  if (Array.isArray(schema.enum) && schema.enum.length > 0) return schema.enum[0];

  if (Array.isArray(schema.allOf)) {
    const merged: Obj = {};
    for (const part of schema.allOf) {
      const value = exampleOf(root, part, path);
      if (isObj(value)) Object.assign(merged, value);
    }
    // Properties given beside allOf belong to the same object.
    if (isObj(schema.properties)) {
      const own = exampleOf(root, { type: "object", properties: schema.properties }, path);
      if (isObj(own)) Object.assign(merged, own);
    }
    return merged;
  }
  for (const key of ["oneOf", "anyOf"]) {
    const options = list(schema[key]).filter((o) => !(isObj(o) && o.type === "null"));
    if (options.length > 0) return exampleOf(root, options[0], path);
  }

  switch (schemaType(schema)) {
    case "object": {
      const out: Obj = {};
      if (isObj(schema.properties)) {
        for (const [name, property] of Object.entries(schema.properties)) {
          out[name] = exampleOf(root, property, path);
        }
      }
      if (isObj(schema.additionalProperties) && Object.keys(out).length === 0) {
        out.key = exampleOf(root, schema.additionalProperties, path);
      }
      return out;
    }
    case "array":
      return [exampleOf(root, schema.items, path)];
    case "string":
      return STRING_EXAMPLES[str(schema.format)] ?? "string";
    case "integer":
    case "number":
      return typeof schema.minimum === "number" ? schema.minimum : 0;
    case "boolean":
      return true;
    default:
      return null;
  }
}

/** A schema's type in a few words, for a table of parameters. */
function describeType(root: Obj, node: unknown): string {
  const named = isObj(node) && typeof node.$ref === "string" ? node.$ref.split("/").pop() : "";
  const schema = deref(root, node);

  if (Array.isArray(schema.enum) && schema.enum.length > 0) {
    return `one of ${schema.enum.map((v) => String(v)).join(", ")}`;
  }

  const type = schemaType(schema);
  if (type === "array") return `array of ${describeType(root, schema.items) || "values"}`;
  if (type === "object") return named || "object";
  if (!type) return named || "";
  return schema.format ? `${type} (${str(schema.format)})` : type;
}

/** A value as the text someone would type into a field for it. */
function asField(value: unknown) {
  if (value === null || value === undefined) return "";
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}

/** A value as the text of a body, for a kind of content. */
function asBody(value: unknown, contentType: string) {
  if (value === undefined || value === null) return "";
  if (typeof value === "string" && !isJson(contentType)) return value;

  if (contentType.includes("x-www-form-urlencoded") && isObj(value)) {
    return Object.entries(value)
      .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(asField(v))}`)
      .join("&");
  }
  return JSON.stringify(value, null, 2);
}

export function isJson(contentType: string) {
  return /[/+]json\b/i.test(contentType);
}

/** The kind of content to show from several on offer: JSON if it is there. */
function pickContent(content: unknown): [string, Obj] | null {
  if (!isObj(content)) return null;
  const kinds = Object.keys(content);
  if (kinds.length === 0) return null;

  const kind = kinds.find(isJson) ?? kinds[0];
  return [kind, isObj(content[kind]) ? (content[kind] as Obj) : {}];
}

/** An example for one kind of content: the one it gives, or one made from its schema. */
function mediaExample(root: Obj, media: Obj): unknown {
  if (media.example !== undefined) return media.example;
  if (isObj(media.examples)) {
    const first = deref(root, Object.values(media.examples)[0]);
    if (first.value !== undefined) return first.value;
  }
  return media.schema === undefined ? undefined : exampleOf(root, media.schema);
}

function parameterOf(root: Obj, node: unknown): ApiParameter | null {
  const p = deref(root, node);
  const where = str(p.in);
  if (!p.name || !["path", "query", "header", "cookie"].includes(where)) return null;

  // OpenAPI 3 keeps the type in a schema; Swagger 2 on the parameter itself.
  const schema = p.schema ?? p;
  const example = p.example !== undefined ? p.example : exampleOf(root, { ...deref(root, schema) });
  const type = describeType(root, schema);

  return {
    name: str(p.name),
    in: where as ApiParameter["in"],
    required: p.required === true || where === "path",
    type,
    description: str(p.description),
    // "string" and 0 say nothing; a real example or a default is worth filling in.
    example: example === "string" || example === 0 || example === true ? "" : asField(example),
  };
}

function operationOf(root: Obj, path: string, method: string, shared: unknown[], op: Obj) {
  // The path's own parameters, replaced by any the operation redefines.
  const parameters = new Map<string, ApiParameter>();
  let body: ApiBody | null = null;

  for (const node of [...shared, ...list(op.parameters)]) {
    const raw = deref(root, node);

    // Swagger 2 sends the body as a parameter.
    if (raw.in === "body") {
      const contentType = str(list(op.consumes)[0] ?? list(root.consumes)[0]) || "application/json";
      body = {
        contentType,
        required: raw.required === true,
        description: str(raw.description),
        example: asBody(exampleOf(root, raw.schema), contentType),
      };
      continue;
    }

    const parameter = parameterOf(root, raw);
    if (parameter) parameters.set(`${parameter.in}:${parameter.name}`, parameter);
  }

  const requestBody = deref(root, op.requestBody);
  const content = pickContent(requestBody.content);
  if (content) {
    const [contentType, media] = content;
    body = {
      contentType,
      required: requestBody.required === true,
      description: str(requestBody.description),
      example: asBody(mediaExample(root, media), contentType),
    };
  }

  const responses: ApiResponse[] = [];
  if (isObj(op.responses)) {
    for (const [status, node] of Object.entries(op.responses)) {
      const response = deref(root, node);
      const produced = pickContent(response.content);

      let contentType = "";
      let example: unknown;
      if (produced) {
        contentType = produced[0];
        example = mediaExample(root, produced[1]);
      } else if (response.schema !== undefined) {
        // Swagger 2.
        contentType = str(list(op.produces)[0] ?? list(root.produces)[0]) || "application/json";
        example = exampleOf(root, response.schema);
      }

      responses.push({
        status,
        description: str(response.description),
        contentType,
        example: asBody(example, contentType || "application/json"),
      });
    }
  }

  // An empty list on the operation means "none", whatever the document says.
  const security = Array.isArray(op.security) ? op.security : list(root.security);

  return {
    id: `${method.toUpperCase()} ${path}`,
    method: method.toUpperCase(),
    path,
    summary: str(op.summary),
    description: str(op.description),
    deprecated: op.deprecated === true,
    secured: security.some((s) => isObj(s) && Object.keys(s).length > 0),
    parameters: [...parameters.values()],
    body,
    responses,
    tag: str(list(op.tags)[0]),
  };
}

/** What every path is under, from where the description says the API is served. */
function basePathOf(root: Obj) {
  let base = str(root.basePath);

  const server = list(root.servers)[0];
  if (isObj(server) && typeof server.url === "string") {
    try {
      base = new URL(server.url, "http://localhost").pathname;
    } catch {
      base = "";
    }
  }

  return base.replace(/\/+$/, "");
}

/**
 * Reads an API's description. Returns null if the text is not one: not JSON,
 * or JSON that is neither OpenAPI 3 nor Swagger 2.
 */
export function parseApiDoc(text: string): ApiDoc | null {
  let root: unknown;
  try {
    root = JSON.parse(text);
  } catch {
    return null;
  }
  if (!isObj(root)) return null;

  const version = str(root.openapi) || str(root.swagger);
  const known = str(root.openapi).startsWith("3.") || str(root.swagger).startsWith("2.");
  if (!known || !isObj(root.paths)) return null;

  const descriptions = new Map<string, string>();
  for (const tag of list(root.tags)) {
    if (isObj(tag) && typeof tag.name === "string")
      descriptions.set(tag.name, str(tag.description));
  }

  const groups = new Map<string, ApiGroup>();
  let count = 0;
  let secured = false;

  for (const [path, node] of Object.entries(root.paths)) {
    const item = deref(root, node);
    const shared = list(item.parameters);

    for (const method of METHODS) {
      if (!isObj(item[method])) continue;

      const { tag, ...operation } = operationOf(root, path, method, shared, item[method]);
      const name = tag || "General";
      if (!groups.has(name)) {
        groups.set(name, { tag: name, description: descriptions.get(name) ?? "", operations: [] });
      }
      groups.get(name)!.operations.push(operation);
      count++;
      secured ||= operation.secured;
    }
  }

  const info = isObj(root.info) ? root.info : {};

  return {
    title: str(info.title) || "API",
    version: str(info.version),
    description: str(info.description),
    kind: `${root.openapi ? "OpenAPI" : "Swagger"} ${version}`,
    basePath: basePathOf(root),
    secured,
    groups: [...groups.values()],
    count,
  };
}

/**
 * The path and headers of a request to an operation, from what was typed
 * for its parameters (keyed "in:name"). Returns the names of required ones
 * left empty in `missing`.
 */
export function buildRequest(doc: ApiDoc, operation: ApiOperation, values: Record<string, string>) {
  const missing: string[] = [];
  const query = new URLSearchParams();
  const headers: Record<string, string> = {};
  let path = operation.path;

  for (const parameter of operation.parameters) {
    const value = (values[`${parameter.in}:${parameter.name}`] ?? "").trim();
    if (!value) {
      if (parameter.required) missing.push(parameter.name);
      continue;
    }

    if (parameter.in === "path") {
      path = path.replaceAll(`{${parameter.name}}`, encodeURIComponent(value));
    } else if (parameter.in === "query") {
      query.append(parameter.name, value);
    } else if (parameter.in === "header") {
      headers[parameter.name] = value;
    }
  }

  const search = query.toString();
  return { path: doc.basePath + path + (search ? `?${search}` : ""), headers, missing };
}
