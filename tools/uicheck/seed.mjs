// Gives a throwaway account sample chats, machines and agents, so that every
// screen of the frontend has something realistic on it to look at: a question
// waiting, a design proposed, an agent at work, a finished build, an error.
//
// It writes the rows straight into the database. Nothing here talks to the
// model, and the machines are rows only: their machine IDs are made up, so
// no container exists and none can be touched. Run it again to start afresh;
// clean.mjs removes the account. The server must be running.
import { randomUUID } from "node:crypto";
import { writeFileSync } from "node:fs";
import { API, EMAIL, PASSWORD, psql } from "./common.mjs";

async function post(path, body) {
  const res = await fetch(API + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return { status: res.status, body: await res.json().catch(() => null) };
}

let auth = await post("/login", { email: EMAIL, password: PASSWORD });
if (auth.status !== 200) {
  auth = await post("/register", {
    email: EMAIL,
    password: PASSWORD,
    display_name: "Dana Reviewer",
    organization: "Design review",
  });
}
if (!auth.body?.token) throw new Error("could not sign in: " + JSON.stringify(auth));
const owner = auth.body.user.id;

const q = (s) => `$seed$${s}$seed$`;
const sql = [];
sql.push(`delete from chats where owner_id='${owner}'; delete from instances where owner_id='${owner}';`);

// Machines. The machine IDs are made up, so nothing here can touch a real container.
const machines = {};
function machine(name, status, stack, cpus, mem, disk, ago) {
  const id = randomUUID();
  machines[name] = { id, name, status, stack, cpus, memory_mb: mem, disk_gb: disk };
  sql.push(
    `insert into instances (id, owner_id, name, status, provider, machine_id, volume_id, cpus, memory_mb, disk_gb, stack, created_at, updated_at, guest_token_hash)
     values ('${id}','${owner}','${name}','${status}','docker','demo-${id}','demo-vol-${id}',${cpus},${mem},${disk},'${stack}', now() - interval '${ago}', now() - interval '${ago}', sha256('${id}'::bytea));`,
  );
  return machines[name];
}
machine("web", "running", "base", 1, 1024, 10, "3 hours");
machine("api", "running", "dotnet", 2, 4096, 20, "3 hours");
machine("db", "running", "data", 1, 2048, 40, "3 hours");
machine("ci", "stopped", "base", 1, 1024, 10, "3 hours");
machine("limiter", "running", "go", 2, 4096, 10, "20 minutes");
machine("scratch-with-a-rather-long-name", "error", "base", 4, 8192, 80, "6 days");

let callN = 0;
const callId = () => `call_${String(callN++).padStart(2, "0")}_seed`;
const call = (name, args) => ({ id: callId(), type: "function", function: { name, arguments: JSON.stringify(args) } });
const mref = (m) => ({ id: m.id, name: m.name, status: m.status, stack: m.stack, cpus: m.cpus, memory_mb: m.memory_mb, disk_gb: m.disk_gb });

function chat(title, status, ago, build) {
  const id = randomUUID();
  const events = [];
  const add = (kind, payload) => events.push({ kind, payload });
  build(add, id);
  sql.push(
    `insert into chats (id, owner_id, title, status, created_at, updated_at) values ('${id}','${owner}',${q(title)},'${status}', now() - interval '${ago}', now() - interval '${ago}');`,
  );
  events.forEach((e, i) =>
    sql.push(
      `insert into chat_events (chat_id, seq, kind, payload) values ('${id}', ${i + 1}, '${e.kind}', ${q(JSON.stringify(e.payload))}::jsonb);`,
    ),
  );
  return id;
}

const brokerage = {
  summary:
    "A crypto brokerage demo where people sign up, hold simulated USD, and buy and sell coins at real market prices, with live balances, order history and a portfolio.",
  structure: "frontend-and-backend",
  repository: "repository-per-component",
  reasons: [
    "Two components because the trading rules belong on a server the browser can't tamper with.",
    "PostgreSQL because balances and orders must change together or not at all.",
    "Prices come from CoinGecko rather than being stored, so the demo is never stale.",
    "A small CI machine builds and tests both repositories on every commit.",
  ],
  nodes: [
    { id: "web", kind: "frontend", name: "Web App", tech: "React + TypeScript (Vite)", why: "Markets, a trade form, portfolio and order history.", machine: "web", stack: "base", size: "small", disk_gb: 10, port: 3000 },
    { id: "api", kind: "backend", name: "Brokerage API", tech: "ASP.NET Core", why: "Auth, accounts, balances, orders and market data.", machine: "api", stack: "dotnet", size: "medium", disk_gb: 20, port: 8080 },
    { id: "db", kind: "database", name: "PostgreSQL", tech: "PostgreSQL 16", why: "Transactional store for users, balances, orders and trades.", machine: "db", stack: "data", size: "small", disk_gb: 40, port: 5432 },
    { id: "coingecko", kind: "external", name: "CoinGecko", tech: "Public market-data API", why: "Source of real prices." },
    { id: "ci", kind: "ci", name: "CI", tech: "Shell + git", why: "Builds and tests both repositories.", machine: "ci", stack: "base", size: "small", disk_gb: 10 },
  ],
  edges: [
    { from: "web", to: "api", label: "REST over HTTP", path: "/api", env: "API_URL" },
    { from: "api", to: "db", label: "SQL", env: "DATABASE_HOST" },
    { from: "api", to: "coingecko", label: "HTTPS market data", env: "COINGECKO_API_URL" },
    { from: "ci", to: "web", label: "git clone, build, test" },
    { from: "ci", to: "api", label: "git clone, build, test" },
  ],
};

const agentRows = [];

// 1. A finished build: every kind of entry a chat can hold.
const built = chat("Crypto brokerage with a .NET API and a React app", "idle", "12 minutes", (add, chatId) => {
  add("user", { text: "want to create a .net backend with a react frontend for a crypto brokerage company" });
  const q1 = call("ask_user", {
    question: "How real should trading be?",
    options: [
      { label: "Trade with simulated money", description: "Real prices, real order and balance logic, no money moves." },
      { label: "Prices and portfolio only", description: "A read-only market view. Fastest to build." },
      { label: "Connect a real exchange", description: "Needs API keys and a lot more care." },
    ],
  });
  add("assistant", { text: "I'll design this with you before anything gets built. A brokerage is a big surface, so let me pin down the two things that actually change the architecture.", tool_calls: [q1] });
  add("tool", { tool_call_id: q1.id, name: "ask_user", content: "Trade with simulated money" });
  const q2 = call("ask_user", { question: "Roughly how many people will use it?", options: [{ label: "Demo / internal, tens of users" }, { label: "Hundreds at once" }] });
  add("assistant", { text: "Good. One more about scale, which sets the machine sizes.", tool_calls: [q2] });
  add("tool", { tool_call_id: q2.id, name: "ask_user", content: "Demo / internal, tens of users" });
  const p = call("propose_architecture", brokerage);
  add("assistant", { text: "Here's the design. Two machines of code plus a database and a small CI rig, nothing more than a demo needs.", tool_calls: [p] });
  add("tool", { tool_call_id: p.id, name: "propose_architecture", content: "The user approved the design. This is what to build:\n\n" + brokerage.summary });
  const mk = ["web", "api", "db", "ci"].map((name) => [name, call("create_machine", { name, stack: machines[name].stack, size: "small", disk_gb: machines[name].disk_gb })]);
  add("assistant", { text: "Design approved. Creating the four machines.", tool_calls: mk.map(([, c]) => c) });
  for (const [name, c] of mk) add("activity", { tool_call_id: c.id, machine: name, text: "Creating the machine" });
  for (const [name, c] of mk) add("tool", { tool_call_id: c.id, name: "create_machine", content: `Machine "${name}" is running and ready.`, machine: mref(machines[name]) });

  const dbTask = call("code_on_machine", { machine: "db", agent: "database", task: "Set up PostgreSQL for the brokerage: a `brokerage` database, a login for the API, and tables for users, balances, orders and trades. Register it as a service so it survives a restart." });
  add("assistant", { text: "All four machines are up. The database first, since the backend depends on it.", tool_calls: [dbTask] });
  for (const line of ["Running pg_lsclusters", "Writing db/init.sql", "Writing db/setup.sh", "Registering service postgresql: postgres -D /var/lib/postgresql/16/main", "Running psql -c '\\dt' brokerage", "Committing: Add schema and setup script"]) add("activity", { tool_call_id: dbTask.id, machine: "db", text: line });
  add("tool", { tool_call_id: dbTask.id, name: "code_on_machine", machine: mref(machines.db), content: "PostgreSQL 16 is running on port 5432 and registered as the `postgresql` service.\n\n- Database `brokerage`, login `brokerage_api`\n- Tables: `users`, `balances`, `orders`, `trades`\n- Schema is in `db/init.sql`; `db/setup.sh` re-creates it from scratch" });

  const apiTask = call("code_on_machine", { machine: "api", agent: "backend", background: true, task: "Build the Brokerage API in ASP.NET Core: sign-up and sign-in, balances, market prices from CoinGecko, and placing buy and sell orders with simulated USD. Serve OpenAPI at /openapi.json." });
  const webTask = call("code_on_machine", { machine: "web", agent: "frontend", background: true, task: "Build the React app with Vite: markets list, trade form, portfolio and order history. Call the API at /api." });
  add("assistant", { text: "Database is ready. The API and the web app can be built at the same time, so both agents start now.", tool_calls: [apiTask, webTask] });
  add("tool", { tool_call_id: apiTask.id, name: "code_on_machine", machine: mref(machines.api), content: "Started in the background." });
  add("tool", { tool_call_id: webTask.id, name: "code_on_machine", machine: mref(machines.web), content: "Started in the background." });
  for (const line of ["Running dotnet new webapi -n Brokerage.Api", "Writing Brokerage.Api/Program.cs", "Writing Brokerage.Api/Orders/OrderService.cs", "Running dotnet build", "Running dotnet test", "Registering service api: dotnet run --urls http://0.0.0.0:8080"]) add("activity", { tool_call_id: apiTask.id, machine: "api", text: line });
  for (const line of ["Running npm create vite@latest web -- --template react-ts", "Writing src/pages/Markets.tsx", "Writing src/pages/Trade.tsx", "Running npm run build"]) add("activity", { tool_call_id: webTask.id, machine: "web", text: line });
  add("message", { from: { machine: "web", agent: "frontend" }, to: { machine: "api", agent: "backend" }, question: "What does POST /orders expect, and what comes back when the balance is too low?", answer: "`POST /orders` takes `{ symbol, side, quantity }`.\n\nWith too little balance it answers **409** with `{ \"error\": \"insufficient_funds\", \"available\": 1250.00 }`." });
  const apiAgent = randomUUID();
  const webAgent = randomUUID();
  add("agent", { agent_id: apiAgent, agent: "backend", tool_call_id: apiTask.id, machine: { id: machines.api.id, name: "api" }, content: "The API is running on port 8080 and registered as the `api` service.\n\n- 14 endpoints, documented at `/openapi.json`\n- 23 tests pass" });
  add("agent", { agent_id: webAgent, agent: "frontend", tool_call_id: webTask.id, machine: { id: machines.web.id, name: "web" }, content: "Error: `npm run build` failed: src/pages/Trade.tsx(41,18): Property 'available' does not exist on type 'OrderError'.", failed: true });
  const show = call("show_preview", { machine: "web", port: 3000, label: "the brokerage" });
  add("assistant", {
    text: "The backend is finished and the frontend hit one type error, which I've sent back to its agent.\n\n## What's running\n\n| Part | Machine | Port | State |\n| --- | --- | --- | --- |\n| Web App | `web` | 3000 | Building |\n| Brokerage API | `api` | 8080 | Running |\n| PostgreSQL | `db` | 5432 | Running |\n\nTo try the API yourself:\n\n```bash\ncurl -X POST https://api.example/orders \\\n  -H 'Content-Type: application/json' \\\n  -d '{\"symbol\":\"BTC\",\"side\":\"buy\",\"quantity\":0.01}'\n```\n\nNext I'd add **price alerts**, or a *limit order* type. Say which and I'll start.",
    tool_calls: [show],
  });
  add("tool", { tool_call_id: show.id, name: "show_preview", machine: mref(machines.web), content: "Shown." });

  agentRows.push({ id: randomUUID(), chatId, machine: "db", name: "database", status: "idle", task: "Set up PostgreSQL for the brokerage: database, login and tables.", background: false, activity: "", report: "PostgreSQL 16 is running on port 5432 and registered as the `postgresql` service.", ago: "40 minutes" });
  agentRows.push({ id: apiAgent, chatId, machine: "api", name: "backend", status: "working", task: "Add price alerts: a table, two endpoints, and a check every minute.", background: true, activity: "Writing Brokerage.Api/Alerts/AlertService.cs", report: "", ago: "2 minutes" });
  agentRows.push({ id: webAgent, chatId, machine: "web", name: "frontend", status: "failed", task: "Build the React app with Vite: markets list, trade form, portfolio and order history.", background: true, activity: "", report: "`npm run build` failed: src/pages/Trade.tsx(41,18): Property 'available' does not exist on type 'OrderError'.", ago: "14 minutes" });
});
sql.push(`insert into architectures (chat_id, owner_id, status, design) values ('${built}','${owner}','approved', ${q(JSON.stringify(brokerage))}::jsonb);`);

// 2. A design waiting to be approved.
const status = {
  summary: "A status page that checks a list of URLs every minute and shows 90 days of uptime for each.",
  structure: "frontend-and-backend",
  repository: "one-repository",
  reasons: ["One Go service does the checking and serves the API, so there is one thing to deploy.", "SQLite is enough for a few hundred checks a minute, and keeps it to two machines."],
  nodes: [
    { id: "page", kind: "frontend", name: "Status page", tech: "Next.js", why: "The public page and an admin form.", machine: "status-web", stack: "base", size: "small", disk_gb: 10, port: 3000 },
    { id: "checker", kind: "backend", name: "Checker", tech: "Go + SQLite", why: "Runs the checks and stores the results.", machine: "status-api", stack: "go", size: "small", disk_gb: 10, port: 8080 },
  ],
  edges: [{ from: "page", to: "checker", label: "JSON over HTTP", path: "/api", env: "API_URL" }],
};
const proposed = chat("Status page with uptime checks", "waiting", "2 minutes", (add) => {
  add("user", { text: "I need a public status page for my side projects. It should ping a few URLs and show uptime history." });
  const p = call("propose_architecture", status);
  add("assistant", { text: "That's small enough to design straight away. Have a look, and change anything on the diagram.", tool_calls: [p] });
});
sql.push(`insert into architectures (chat_id, owner_id, status, design) values ('${proposed}','${owner}','proposed', ${q(JSON.stringify(status))}::jsonb);`);

// 3. A question waiting to be answered.
chat("Rename photos by the date they were taken", "waiting", "35 minutes", (add) => {
  add("user", { text: "Write me a small tool that renames all the photos in a folder by the date they were taken." });
  const c = call("ask_user", {
    question: "What should it be written in?",
    options: [
      { label: "Python", description: "Shortest to write, and reads EXIF with one library." },
      { label: "Go", description: "A single binary you can copy anywhere." },
      { label: "Node.js", description: "If the rest of your tools are JavaScript." },
    ],
  });
  add("assistant", { text: "Happy to. One thing decides the rest:", tool_calls: [c] });
});

// 4. An agent at work right now.
chat("Rate limiter service in Go", "running", "20 seconds", (add) => {
  add("user", { text: "Make a rate limiter service in Go with a token bucket, and a load test that proves it holds." });
  const mk = call("create_machine", { name: "limiter", stack: "go", size: "medium", disk_gb: 10 });
  add("assistant", { text: "One machine is enough for this.", tool_calls: [mk] });
  add("tool", { tool_call_id: mk.id, name: "create_machine", content: "Machine is running and ready.", machine: mref(machines.limiter) });
  const t = call("code_on_machine", { machine: "limiter", agent: "coder", task: "Write a token-bucket rate limiter as an HTTP service in Go, with tests and a load test using vegeta." });
  add("assistant", { text: "", tool_calls: [t] });
  for (const line of ["Running go mod init limiter", "Writing bucket/bucket.go", "Writing bucket/bucket_test.go", "Running go test -race ./...", "Writing cmd/limiter/main.go", "Running go vet ./... && go build ./cmd/limiter"]) add("activity", { tool_call_id: t.id, machine: "limiter", text: line });
});

// 5. Older chats, for the sidebar's groups, and one that ended badly.
chat("Fix the flaky login test", "idle", "1 day 2 hours", (add) => {
  add("user", { text: "The login test fails about one run in five. Find out why." });
  add("assistant", { text: "It waits a fixed 200 ms for the session cookie instead of waiting for the response. I replaced the sleep with a wait on the request; 50 runs in a row now pass." });
});
chat("A very long chat title that keeps going so the sidebar has to cut it off somewhere sensible", "idle", "3 days", (add) => {
  add("user", { text: "Can you set up a Python project with a test that passes?" });
  add("error", { message: "model returned 400 Bad Request: Invalid schema for function 'list_machines': null is not of type \"array\"" });
  add("user", { text: "try again" });
  const c = call("list_machines", {});
  add("assistant", { text: "", tool_calls: [c] });
  add("error", { message: "Stopped.", stopped: true });
});
chat("Landing page for the bakery", "idle", "5 days", (add) => {
  add("user", { text: "A one-page site for a bakery with opening hours and a map." });
  add("assistant", { text: "Done. It's a single static page served by Caddy on `web`." });
});
chat("Scrape the council's planning notices", "idle", "19 days", (add) => {
  add("user", { text: "Scrape the planning notices from the council site every morning into a CSV." });
  add("assistant", { text: "A cron job on `scratch` now runs the scraper at 06:00 and appends to `notices.csv`." });
});

for (const a of agentRows) {
  const done = a.status === "working" ? "null" : `now() - interval '${a.ago}'`;
  sql.push(
    `insert into agents (id, owner_id, chat_id, instance_id, name, status, task, background, activity, report, started_at, finished_at, updated_at)
     values ('${a.id}','${owner}','${a.chatId}','${machines[a.machine].id}',${q(a.name)},'${a.status}',${q(a.task)},${a.background},${q(a.activity)},${q(a.report)}, now() - interval '${a.ago}' - interval '6 minutes', ${done}, now() - interval '${a.ago}');`,
  );
}
for (const [m, name, command, dir, port] of [
  ["db", "postgresql", "postgres -D /var/lib/postgresql/16/main", "/home/coder/db", 5432],
  ["api", "api", "dotnet run --urls http://0.0.0.0:8080", "/home/coder/Brokerage.Api", 8080],
  ["web", "web", "npm run preview -- --host --port 3000", "/home/coder/web", 3000],
]) {
  sql.push(`insert into services (instance_id, name, command, directory, port) values ('${machines[m].id}',${q(name)},${q(command)},${q(dir)},${port});`);
}

psql("begin;\n" + sql.join("\n") + "\ncommit;");
const out = { session: auth.body, chats: psql(`select id || '|' || title || '|' || status from chats where owner_id='${owner}' order by updated_at desc`).trim().split("\n"), machines };
writeFileSync(new URL("./seed.json", import.meta.url), JSON.stringify(out, null, 2));
console.log(out.chats.join("\n"));
console.log("user", owner, auth.body.user.email);
