// What the scripts here share: where the app is, who they sign in as, and a
// way to run SQL against the platform's database.
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, rmSync } from "node:fs";

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

/** Runs a docker command that is allowed to find nothing to do. */
function docker(...args) {
  try {
    execFileSync("docker", args, { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

/**
 * Removes a test account and everything it owns, in the order docs/STATUS.md
 * gives: containers, volumes and the network first, then the rows. Returns a
 * sentence saying what was removed. Only accounts named as the project's rule
 * for test accounts asks can be removed this way.
 */
export function removeAccount(email) {
  if (!/^tmpcheck-[a-z0-9-]+@example\.com$/.test(email)) {
    throw new Error(`${email} is not a test account (tmpcheck-<something>@example.com)`);
  }

  const user = psql(`select id from users where email = '${email}'`).trim();
  if (!user) return `No account ${email}; nothing to remove.`;

  const instances = psql(`select id from instances where owner_id = '${user}'`)
    .trim()
    .split("\n")
    .filter(Boolean);

  let real = 0;
  for (const id of instances) {
    // Asked first: `docker rm -f` reports success for a name that isn't there.
    if (docker("container", "inspect", `coder-${id}`)) {
      docker("rm", "-f", `coder-${id}`);
      real++;
    }
    docker("volume", "rm", `coder-${id}`);
  }
  docker("network", "rm", `coder-user-${user}`);

  psql(
    `delete from chats where owner_id = '${user}';
     delete from instances where owner_id = '${user}';
     delete from users where id = '${user}';`,
  );

  // The session in it, if it was this account's, is for nobody now.
  if (email === EMAIL) rmSync(SEED_FILE, { force: true });

  return (
    `Removed ${email}: ${instances.length} machine rows` +
    (real ? `, ${real} of them with a real container` : "") +
    ", its chats, and the account."
  );
}
