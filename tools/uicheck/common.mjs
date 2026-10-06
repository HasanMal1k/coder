// What the scripts here share: where the app is, who they sign in as, and a
// way to run SQL against the platform's database.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";

/** The frontend. It has to be this host: see web/README.md, "Running". */
export const BASE = process.env.UICHECK_WEB_URL ?? "http://coder.localhost:3000";
/** The server. */
export const API = process.env.UICHECK_API_URL ?? "http://localhost:8080";

// Named as the project's rule for test accounts asks (CLAUDE.md).
export const EMAIL = "tmpcheck-design@example.com";
export const PASSWORD = "tmpcheck-design-2026";

const SEED_FILE = new URL("./seed.json", import.meta.url);

/** DATABASE_URL, from the environment or the repository's .env. */
function databaseUrl() {
  if (process.env.DATABASE_URL) return process.env.DATABASE_URL;
  const env = readFileSync(new URL("../../.env", import.meta.url), "utf8");
  const found = env.match(/^DATABASE_URL=(.*)$/m);
  if (!found) throw new Error("DATABASE_URL is not set, and .env has none");
  return found[1].trim();
}

/** Runs SQL with psql inside the Postgres container, and returns what it printed. */
export function psql(sql) {
  return execFileSync(
    "docker",
    ["exec", "-i", "coder-postgres-1", "psql", databaseUrl(), "-At", "-v", "ON_ERROR_STOP=1"],
    { input: sql, encoding: "utf8" },
  );
}

/** What seed.mjs left behind: the account's session, its chats and its machines. */
export function seeded() {
  if (!existsSync(SEED_FILE)) throw new Error("No seed.json. Run `node seed.mjs` first.");
  return JSON.parse(readFileSync(SEED_FILE, "utf8"));
}
