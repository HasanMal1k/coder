// Builds a whole project in a real browser, on real machines, with the
// stand-in for the model (tools/standin) answering in the model's place, and
// says what held and what didn't. This is the end-to-end check: sign-up, the
// question, the design and a change to it, approval, four machines, four
// coding agents doing real work, and the result in every tab.
//
//   node build.mjs                 run it, then remove the account and machines
//   node build.mjs --keep          leave them, to look at afterwards
//   node build.mjs --shots shots   also save a picture of each stage
//
// It needs Postgres, the frontend, the machine images (`make images`), the
// stand-in, and the server started against the stand-in:
//
//   (cd tools/standin && go run .) &
//   DEEPSEEK_API_KEY=standin LLM_BASE_URL=http://localhost:9099/v1 LLM_MODEL=standin make run
//
// It refuses to start a chat on a server whose model is anything else, so it
// cannot spend credits by mistake. A run takes about six minutes.
import { mkdirSync } from "node:fs";
import { chromium, devices } from "playwright";
import { API, BASE, removeAccount } from "./common.mjs";

const EMAIL = "tmpcheck-build@example.com";
const OTHER_EMAIL = "tmpcheck-build-other@example.com";
const PASSWORD = "tmpcheck-build-2026";
/** What the server must say its model is. */
const STANDIN = "standin";

const keep = process.argv.includes("--keep");
const shotsAt = process.argv.indexOf("--shots");
const shots = shotsAt >= 0 ? process.argv[shotsAt + 1] : null;
if (shots) mkdirSync(shots, { recursive: true });

const results = [];
// Things known to be wrong with the app that the run met and stepped round.
// They do not fail it, and they are not hidden either.
const notes = [];
function note(text) {
  notes.push(text);
  console.log(`NOTE  ${text}`);
}
function check(name, ok, detail = "") {
  const line = `${ok ? "PASS" : "FAIL"}  ${name}${detail ? "  (" + detail + ")" : ""}`;
  results.push(line);
  console.log(line);
}

/** Waits until what() is truthy, and returns it; false if it never is. */
async function until(what, ms = 15_000, every = 250) {
  const deadline = Date.now() + ms;
  for (;;) {
    try {
      const got = await what();
      if (got) return got;
    } catch {
      // Not there yet.
    }
    if (Date.now() > deadline) return false;
    await new Promise((r) => setTimeout(r, every));
  }
}

async function call(method, path, token, body) {
  const res = await fetch(API + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(token && { Authorization: "Bearer " + token }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return { status: res.status, body: await res.json().catch(() => null) };
}

// Start from nothing, whatever an earlier run left.
removeAccount(EMAIL);
removeAccount(OTHER_EMAIL);

const browser = await chromium.launch();
const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
const page = await context.newPage();
const errors = [];
page.on("pageerror", (e) => errors.push(String(e).slice(0, 200)));

const shot = async (name) => shots && page.screenshot({ path: `${shots}/${name}.png` });
const pane = page.getByRole("complementary", { name: "Project" });
const tab = (name) => pane.getByRole("tab", { name });
const visible = (locator, ms) => until(() => locator.first().isVisible(), ms);
// Some of the chat is folded away (a task's report, an exchange): there, but
// not showing until it is opened.
const present = async (locator) => (await locator.count()) > 0;

try {
  // 1. Sign up, through the form.
  await page.goto(BASE + "/register", { waitUntil: "domcontentloaded" });
  await page.getByLabel("Name").fill("Build Check");
  await page.getByLabel("Email").fill(EMAIL);
  await page.getByLabel("Password", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Create account" }).click();
  await page.waitForURL(BASE + "/", { timeout: 20_000 }).catch(() => {});
  check("sign-up lands on a new chat", new URL(page.url()).pathname === "/", page.url());

  const session = await page.evaluate(() => JSON.parse(localStorage.getItem("coder.session")));
  const token = session?.token;
  check("signed in", Boolean(token));

  // Never the real model.
  const assistant = await call("GET", "/assistant", token);
  if (!assistant.body?.enabled || assistant.body.model !== STANDIN) {
    throw new Error(
      `the server's model is ${JSON.stringify(assistant.body?.model)}, not "${STANDIN}". ` +
        "Start it against the stand-in (see the top of this file); nothing was sent.",
    );
  }

  // 2. A new chat becomes the chat's own page, with its panel, empty so far.
  check("new chat: no panel yet", (await pane.count()) === 0);
  await page.getByLabel("Message the assistant").fill("Build me an online shop");
  await page.keyboard.press("Enter");
  await page.waitForURL(/\/chats\//, { timeout: 20_000 }).catch(() => {});
  const chatPath = new URL(page.url()).pathname;
  check("the chat has its own address", /^\/chats\/[0-9a-f-]{36}$/.test(chatPath), chatPath);
  check("the panel arrives with the chat", await visible(pane));
  check(
    "graph: says the design will appear",
    await visible(pane.getByText("The design appears here")),
  );
  await tab("Frontend").click();
  check("frontend: nothing to show yet", await visible(pane.getByText("No frontend to show yet")));
  await tab("Backend").click();
  check("backend: nothing to show yet", await visible(pane.getByText("No backend to show yet")));
  await tab("Dashboard").click();
  check("dashboard: no machines yet", await visible(pane.getByText("No machines yet")));
  await tab("Graph").click();

  // 3. The question.
  const answers = page.getByRole("group", { name: "Answers" });
  check("the assistant asks a question", await visible(answers));
  check("with two answers to pick from", (await answers.getByRole("button").count()) === 2);
  check(
    "its own pick is marked",
    await answers.getByRole("button").first().getByText("Suggested").isVisible(),
  );
  await answers.getByRole("button", { name: /A few dozen/ }).click();

  // 4. The design, proposed.
  check("a design is proposed in the chat", await visible(page.getByText("Proposed design")));
  check("and on the graph", await visible(pane.getByText("Proposed", { exact: true })));
  check(
    "four components on four machines",
    await visible(pane.getByText("4 components on 4 machines")),
  );
  const node = (label) => pane.getByLabel(label, { exact: true });
  for (const label of [
    "Storefront, Frontend",
    "Orders API, Backend",
    "Catalogue, Database",
    "Checks, CI",
  ]) {
    check(`graph draws ${label.split(",")[0]}`, await visible(node(label)));
  }
  check(
    "the API's machine is small",
    await visible(node("Orders API, Backend").getByText(/Small/)),
  );
  await shot("1-proposed");

  // 5. Asking for a change gets the design proposed again, changed.
  await page.getByLabel("Message the assistant").fill("Make the API machine medium");
  await page.keyboard.press("Enter");
  check(
    "the chat shows what was asked for",
    await visible(page.getByText("You asked for a change:")),
  );
  check(
    "the first proposal is marked as superseded",
    await visible(page.getByText("Design, before your changes")),
  );
  check(
    "the new design has a medium API machine",
    await visible(node("Orders API, Backend").getByText(/Medium/)),
  );
  check(
    "it is still only a proposal",
    await pane.getByText("Proposed", { exact: true }).isVisible(),
  );

  // 6. Approval, on the graph.
  await pane.getByRole("button", { name: "Approve and build" }).click();
  check("the graph says approved", await visible(pane.getByText("Approved", { exact: true })));
  check("the chat says approved", await visible(page.getByText("Design approved")));

  // 7. Building. The dashboard fills as machines are made and agents start.
  await tab("Dashboard").click();
  const machines = pane.getByRole("region", { name: "Machines in this chat" });
  const card = (name) => machines.getByLabel(name, { exact: true });
  const made = await until(
    async () => {
      for (const name of ["web", "api", "db", "ci"])
        if (!(await card(name).isVisible())) return false;
      return true;
    },
    180_000,
    1000,
  );
  check("dashboard: the four machines appear", Boolean(made));
  check(
    "a run in progress can be stopped",
    await page.getByRole("button", { name: "Stop", exact: true }).isVisible(),
  );

  const sawWorking = await until(() => pane.getByText(/\d working/).isVisible(), 240_000, 500);
  check("dashboard: an agent is shown working", Boolean(sawWorking));
  await shot("2-building");

  await tab("Graph").click();
  const lit = await until(
    () =>
      pane
        .getByText(/is working|: (Writing|Running|Registering|Asking)/)
        .first()
        .isVisible(),
    120_000,
    300,
  );
  check("graph: the component being worked on says so", Boolean(lit));

  // 8. Built. The checks run in the background, so the summary comes after
  // the assistant has been set going again by their finishing.
  const built = await visible(page.getByText("The shop is built and running."), 12 * 60_000);
  check("the assistant reports the shop built", Boolean(built));
  await until(() => page.getByRole("button", { name: "Send" }).isVisible(), 20_000);
  await shot("3-built");

  const log = (machine) => page.locator(`ol[aria-label="What the coding agent did on ${machine}"]`);
  // A line is drawn in columns (its kind, then what was done); read it as one.
  const lines = async (machine) =>
    (await log(machine).locator("li").allInnerTexts())
      .map((line) => line.replace(/\s+/g, " "))
      .join("\n");
  const apiLog = await lines("api");
  check(
    "api agent: wrote its server twice",
    (apiLog.match(/Writing api\/server\.py/g) ?? []).length === 2,
    apiLog.replace(/\n/g, " | ").slice(0, 300),
  );
  check(
    "api agent: registered its service twice",
    (apiLog.match(/Registering service api/g) ?? []).length === 2,
  );
  check(
    "api agent: was told its description was missing, and the new program was the one checked",
    await present(page.getByText("Its API description is served at /openapi.json (4 paths).")),
  );
  check(
    "database agent: three products",
    await present(page.getByText(/the products table has 3 rows/)),
  );

  const exchange = page
    .locator("details")
    .filter({ hasText: /storefront\s+on\s+web\s+asked\s+api\s+on\s+api/ });
  check("the agents' exchange is in the chat", (await exchange.count()) === 1);
  await exchange.locator("summary").click();
  check(
    "with the question and the answer",
    await exchange
      .getByText(/price_cents/)
      .first()
      .isVisible(),
  );

  check(
    "the chat marks where the background agent finished",
    await page
      .getByRole("status")
      .filter({ hasText: /ci\s+on\s+ci\s+finished/ })
      .isVisible(),
  );
  check("with its report", await present(page.getByText(/api: PASS/)));
  check(
    "the summary's table is drawn",
    (await page.locator("th", { hasText: "What runs there" }).count()) === 1,
  );
  check(
    "and its code, with a copy button",
    (await page.getByRole("button", { name: "Copy code" }).count()) >= 1,
  );
  check(
    "a button opens the storefront",
    await page
      .getByRole("button", { name: /Open Storefront/ })
      .first()
      .isVisible(),
  );

  // 9. The graph, built.
  for (const [label, port] of [
    ["Storefront, Frontend", 3000],
    ["Orders API, Backend", 8000],
    ["Catalogue, Database", 5432],
  ]) {
    check(
      `graph: ${label.split(",")[0]} is live on :${port}`,
      await visible(node(label).getByText(`Live on :${port}`), 20_000),
    );
  }
  check(
    "frontend and backend tabs show something is running",
    (await pane.getByRole("tablist").getByLabel("Running").count()) === 2,
  );
  await shot("4-graph");

  // 10. The page, live, calling the API on another machine through its own
  // address.
  await tab("Frontend").click();
  const frame = pane.frameLocator('iframe[title="Storefront, live"]');
  check(
    "frontend: the page loads in the frame",
    await visible(frame.getByRole("heading", { name: "The shop" }), 30_000),
  );
  check(
    "frontend: it lists the products from the API",
    await visible(frame.getByText("Notebook $8.00"), 20_000),
  );
  await frame.getByRole("button", { name: "Order Mug" }).click();
  check(
    "frontend: an order goes through",
    await visible(frame.getByText("Order 1 taken: Mug, $12.00")),
  );
  await shot("5-frontend");

  // 11. The API, drawn from its own description, and tried.
  await tab("Backend").click();
  const named = pane.getByRole("heading", { name: /Orders API/ });
  const lookAgain = pane.getByRole("button", { name: "Look again" });
  await until(async () => (await named.isVisible()) || (await lookAgain.isVisible()), 30_000);
  if (await lookAgain.isVisible()) {
    // The tab reads the description once, when the backend first answers.
    // This backend first came up without one, and the tab may have looked
    // then (docs/STATUS.md, issue 24).
    note(
      "backend tab still said the API had no description after it was added; pressed Look again",
    );
    await lookAgain.click();
  }
  check("backend: the API is named", await visible(named, 30_000));
  check("backend: five operations", await visible(pane.getByText("5 operations")));
  check(
    "backend: read from where every backend serves it",
    await visible(pane.getByText(/OpenAPI 3\.0\.3, from/)),
  );
  for (const group of ["Catalogue", "Orders", "Service"]) {
    check(`backend: group ${group}`, await pane.getByRole("region", { name: group }).isVisible());
  }

  const operation = (method, path) =>
    pane.locator("li").filter({
      has: page.locator("button[aria-expanded]", {
        hasText: new RegExp(`^${method}\\s*${path.replace(/[{}/]/g, "\\$&")}(?!/)`),
      }),
    });
  // What came back, as distinct from the spinner while it is on its way.
  const answer = (item) => item.locator('div[role="status"]');
  async function open(method, path) {
    const item = operation(method, path).first();
    await item.locator("button[aria-expanded]").click();
    return item;
  }
  async function send(item, status) {
    await item.getByRole("button", { name: "Send" }).click();
    await until(async () => (await answer(item).innerText()).startsWith(String(status)), 15_000);
    return answer(item).innerText();
  }

  const one = await open("GET", "/api/products/{id}");
  check(
    "backend: a path parameter is filled in from the description",
    (await one.locator("input").first().inputValue()) === "2",
  );
  const oneAnswer = await send(one, 200);
  check(
    "backend: path parameter",
    oneAnswer.startsWith("200") && oneAnswer.includes("Notebook"),
    oneAnswer.slice(0, 60).replace(/\s+/g, " "),
  );

  const some = await open("GET", "/api/products");
  await some.locator("input").first().fill("1000");
  const someAnswer = await send(some, 200);
  check(
    "backend: query parameter",
    someAnswer.includes("Pen") && !someAnswer.includes("Mug"),
    someAnswer.slice(0, 60).replace(/\s+/g, " "),
  );

  const place = await open("POST", "/api/orders");
  check(
    "backend: a body is suggested",
    (await place.locator("textarea").inputValue()).includes("product_id"),
  );
  const placeAnswer = await send(place, 201);
  check(
    "backend: body",
    placeAnswer.startsWith("201") && placeAnswer.includes("2400"),
    placeAnswer.slice(0, 60).replace(/\s+/g, " "),
  );

  const listed = await open("GET", "/api/orders");
  check(
    "backend: the operation is marked as needing credentials",
    await listed.getByLabel("Needs credentials").isVisible(),
  );
  const refused = await send(listed, 401);
  check(
    "backend: refused without credentials",
    refused.startsWith("401"),
    refused.slice(0, 40).replace(/\s+/g, " "),
  );
  await pane.getByPlaceholder("Bearer …").fill("Bearer shop-admin");
  const allowed = await send(listed, 200);
  check(
    "backend: allowed with them",
    allowed.startsWith("200") && allowed.includes("total_cents"),
    allowed.slice(0, 40).replace(/\s+/g, " "),
  );
  await shot("6-backend");

  // 12. Tabs keep what was in them.
  await tab("Frontend").click();
  check(
    "frontend: still where it was left",
    await frame.getByText("Order 1 taken: Mug, $12.00").isVisible(),
  );
  await tab("Backend").click();
  check("backend: still where it was left", (await answer(listed).innerText()).startsWith("200"));

  // 13. The dashboard, at rest.
  await tab("Dashboard").click();
  const stat = async (label) =>
    (await pane.locator("dt", { hasText: label }).locator("..").innerText()).replace(/\s+/g, " ");
  check(
    "dashboard: four machines running",
    /4\s*of 4/.test(await stat("Machines running")),
    await stat("Machines running"),
  );
  check(
    "dashboard: no agent working",
    /0\s*of 4/.test(await stat("Agents working")),
    await stat("Agents working"),
  );
  await shot("7-dashboard");

  // 14. What the request endpoint refuses.
  const mine = (await call("GET", "/instances", token)).body;
  const api = (mine.instances ?? mine).find((i) => i.name === "api");
  const request = (who, id, port, body) =>
    call("POST", `/instances/${id}/ports/${port}/request`, who, body);
  check(
    "request: a port that is no port",
    (await request(token, api.id, 0, { method: "GET", path: "/" })).status === 400,
  );
  check(
    "request: a method that is none",
    (await request(token, api.id, 8000, { method: "FETCH", path: "/" })).status === 400,
  );
  check(
    "request: without signing in",
    (await request(null, api.id, 8000, { method: "GET", path: "/" })).status === 401,
  );
  const other = (
    await call("POST", "/register", null, {
      email: OTHER_EMAIL,
      password: PASSWORD,
      display_name: "Someone Else",
    })
  ).body;
  const theirs = await request(other.token, api.id, 8000, { method: "GET", path: "/api/orders" });
  check("request: someone else's machine", theirs.status === 404, String(theirs.status));

  // 15. The panel can be hidden and brought back, on the tab it was on.
  await pane.getByRole("button", { name: "Hide the panel" }).click();
  check("the panel hides", (await pane.count()) === 0);
  await page.getByRole("button", { name: "Panel" }).click();
  check("and comes back on the same tab", await visible(pane.getByText("Machines in this chat")));

  // 16. A second chat starts clean, and the first is still listed.
  await page.getByRole("link", { name: "New chat" }).click();
  await page.waitForURL(BASE + "/", { timeout: 10_000 }).catch(() => {});
  check("a new chat has no panel", (await pane.count()) === 0);
  check(
    "the first chat is listed",
    await visible(
      page.getByRole("navigation", { name: "Chats" }).getByRole("link", { name: /online shop/i }),
    ),
  );

  check("no errors on the page", errors.length === 0, errors.join(" | "));

  // 17. A phone.
  const phone = await browser.newContext({ ...devices["iPhone 13"] });
  await phone.addInitScript(
    (s) => localStorage.setItem("coder.session", JSON.stringify(s)),
    session,
  );
  const small = await phone.newPage();
  await small.goto(BASE + chatPath, { waitUntil: "domcontentloaded" });
  check(
    "phone: the chat opens",
    await until(() => small.getByText("The shop is built and running.").isVisible(), 20_000),
  );
  const overflow = await small.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  check("phone: the page does not scroll sideways", overflow <= 0, `overflow ${overflow}px`);
  await small.getByRole("button", { name: "Panel" }).click();
  check(
    "phone: the panel opens over the chat",
    await until(() => small.getByRole("tab", { name: "Graph" }).isVisible()),
  );
  if (shots) await small.screenshot({ path: `${shots}/8-phone.png` });
  await phone.close();
} catch (err) {
  const showing = await pane
    .innerText()
    .then((text) => " | the panel showed: " + text.replace(/\s+/g, " ").slice(0, 300))
    .catch(() => "");
  check(
    "the run finished",
    false,
    String(err.message ?? err)
      .split("\n")[0]
      .slice(0, 300) + showing,
  );
  await shot("failed").catch(() => {});
} finally {
  await browser.close();

  if (keep) {
    console.log(`\nLeft in place. Remove with: node clean.mjs ${EMAIL} ${OTHER_EMAIL}`);
  } else {
    console.log("\n" + removeAccount(EMAIL));
    console.log(removeAccount(OTHER_EMAIL));
  }
}

const failed = results.filter((r) => r.startsWith("FAIL")).length;
console.log(
  `\n${results.length - failed} passed, ${failed} failed` +
    (notes.length ? `, ${notes.length} known issue${notes.length === 1 ? "" : "s"} met` : ""),
);
process.exitCode = failed ? 1 : 0;
