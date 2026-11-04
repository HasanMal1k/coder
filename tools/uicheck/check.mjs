// Drives the frontend in a real browser, signed in as the account seed.mjs
// made, and says what held and what didn't. Run seed.mjs first: one of the
// checks deletes a seeded chat.
//
//   node check.mjs                 everything that never reaches the model
//   node check.mjs --start-chat    also start a chat, which sends a message
//
// --start-chat makes the server call its model. Only pass it when the server
// was started against a stand-in or a dead address, never the real one:
//
//   DEEPSEEK_API_KEY=mock-key LLM_BASE_URL=http://localhost:9099/v1 go run ./cmd/server
import { chromium, devices } from "playwright";
import { BASE, seeded } from "./common.mjs";

const seed = seeded();
const chatPath = (t) => "/chats/" + seed.chats.find((c) => c.split("|")[1].startsWith(t)).split("|")[0];
const results = [];
const check = (name, ok, detail = "") => results.push(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? "  (" + detail + ")" : ""}`);

const browser = await chromium.launch();
async function open(path, options = {}) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, ...options });
  await context.grantPermissions(["clipboard-read", "clipboard-write"]).catch(() => {});
  await context.addInitScript((s) => localStorage.setItem("coder.session", JSON.stringify(s)), seed.session);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e).slice(0, 160)));
  await page.goto(BASE + path, { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(2500);
  return { page, context, errors };
}

// 1. New chat: the panel is not there, the box has focus, a suggestion fills it.
{
  const { page, context, errors } = await open("/");
  check("new chat: no panel beside it", (await page.getByRole("complementary", { name: "Project" }).count()) === 0);
  check("new chat: no Panel button", (await page.getByRole("button", { name: "Panel" }).count()) === 0);
  check("new chat: typing box has focus on a desktop", await page.evaluate(() => document.activeElement?.tagName === "TEXTAREA"));
  check("new chat: send is disabled with nothing typed", await page.getByRole("button", { name: "Send" }).isDisabled());
  await page.getByRole("button", { name: "Set up a Python project with a test that passes" }).click();
  check("suggestion fills the box", (await page.locator("textarea").inputValue()) === "Set up a Python project with a test that passes");
  check("send is enabled once there is text", await page.getByRole("button", { name: "Send" }).isEnabled());
  check("no page errors on new chat", errors.length === 0, errors.join(" | "));
  await context.close();
}

// 2. Skip link: first Tab lands on it, and it goes to the page.
{
  const { page, context } = await open("/machines");
  await page.keyboard.press("Tab");
  const first = await page.evaluate(() => document.activeElement?.textContent?.trim());
  check("first Tab reaches the skip link", first === "Skip to the page", String(first));
  const visible = await page.evaluate(() => { const r = document.activeElement.getBoundingClientRect(); return r.width > 20 && r.height > 20; });
  check("skip link is visible when focused", visible);
  await page.keyboard.press("Enter");
  check("skip link moves focus to the page", await page.evaluate(() => document.activeElement?.id === "main"));
  await context.close();
}

// 3. A built chat: scrolled to its end, copy works, log is structured, table doesn't break words.
{
  const { page, context, errors } = await open(chatPath("Crypto"));
  const atEnd = await page.evaluate(() => {
    const el = [...document.querySelectorAll("div")].find((d) => d.scrollHeight > d.clientHeight + 200 && getComputedStyle(d).overflowY === "auto");
    return el ? Math.round(el.scrollHeight - el.scrollTop - el.clientHeight) : -1;
  });
  check("chat opens scrolled to its end", atEnd >= 0 && atEnd <= 2, `distance from end ${atEnd}px`);
  // The seeded chat ends with a message to @ava, which it is still answering.
  const last = await page.getByText("is on it").last().boundingBox();
  const box = await page.locator("textarea").boundingBox();
  check("last message clears the typing box", last && box && last.y + last.height < box.y, last && box ? `gap ${Math.round(box.y - last.y - last.height)}px` : "");
  await page.getByRole("button", { name: "Copy code" }).first().click();
  const copied = await page.evaluate(() => navigator.clipboard.readText()).catch((e) => "ERR " + e);
  check("copy button copies the code", copied.startsWith("curl -X POST") && copied.includes("quantity"), copied.slice(0, 40));
  const cell = await page.evaluate(() => { const th = [...document.querySelectorAll("th")].find((t) => t.textContent === "Machine"); const r = th.getBoundingClientRect(); return { h: Math.round(r.height), wrap: getComputedStyle(th).overflowWrap }; });
  check("table header stays on one line", cell.h < 40, JSON.stringify(cell));
  const lines = await page.locator("ol[aria-label^='What the coding agent did'] li").count();
  check("agent log lines are drawn", lines > 5, `${lines} lines`);
  check("panel is beside a real chat", (await page.getByRole("complementary", { name: "Project" }).count()) === 1);
  check("no page errors in a built chat", errors.length === 0, errors.join(" | "));
  await context.close();
}

// 4. Delete a chat: asks first; Cancel keeps it; confirming removes it.
{
  const { page, context } = await open(chatPath("Landing"));
  const row = page.getByRole("listitem").filter({ hasText: "Landing page for the bakery" });
  await row.hover();
  await page.getByRole("button", { name: "More for Landing page for the bakery" }).click();
  await page.getByRole("menuitem", { name: /Delete chat/ }).click();
  await page.waitForTimeout(400);
  const dialog = page.getByRole("dialog", { name: "Delete chat" });
  check("delete asks first", await dialog.isVisible());
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await page.waitForTimeout(400);
  check("Cancel keeps the chat", (await page.getByRole("link", { name: "Landing page for the bakery" }).count()) === 1);
  await row.hover();
  await page.getByRole("button", { name: "More for Landing page for the bakery" }).click();
  await page.getByRole("menuitem", { name: /Delete chat/ }).click();
  await page.getByRole("dialog", { name: "Delete chat" }).getByRole("button", { name: "Delete chat" }).click();
  await page.waitForTimeout(1500);
  check("confirming removes the chat", (await page.getByRole("link", { name: "Landing page for the bakery" }).count()) === 0);
  check("and goes to a new chat", new URL(page.url()).pathname === "/", page.url());
  await context.close();
}

// 5. A phone: opening a chat does not raise the keyboard; the table scrolls, not the page.
{
  const { page, context } = await open(chatPath("Crypto"), { ...devices["iPhone 13"] });
  check("phone: typing box is not focused on open", await page.evaluate(() => document.activeElement?.tagName !== "TEXTAREA"));
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  check("phone: page does not scroll sideways", overflow <= 0, `overflow ${overflow}px`);
  await context.close();
}

// 6. Reduced motion: a spinner does not spin for ever.
{
  const { page, context } = await open(chatPath("Rate"), { reducedMotion: "reduce" });
  const anim = await page.evaluate(() => { const el = document.querySelector(".animate-spin"); if (!el) return null; const s = getComputedStyle(el); return { count: s.animationIterationCount, duration: s.animationDuration }; });
  check("reduced motion: spinner runs once, not for ever", anim && anim.count === "1", JSON.stringify(anim));
  await context.close();
}

// 7. Starting a chat: the page becomes the chat's, with its panel. Only on request.
if (process.argv.includes("--start-chat")) {
  const { page, context, errors } = await open("/");
  await page.keyboard.type("Make a page that says hello");
  await page.keyboard.press("Enter");
  await page.waitForURL(/\/chats\//, { timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(3000);
  check("start: goes to the chat's own address", /^\/chats\//.test(new URL(page.url()).pathname), page.url());
  check("start: the message is shown", (await page.getByText("Make a page that says hello").count()) > 0);
  check("start: the panel arrives with the chat", (await page.getByRole("complementary", { name: "Project" }).count()) === 1);
  check("start: no page errors", errors.length === 0, errors.join(" | "));
  await context.close();
}

await browser.close();
console.log(results.join("\n"));
const failed = results.filter((r) => r.startsWith("FAIL")).length;
console.log(`\n${results.length - failed} passed, ${failed} failed`);
process.exitCode = failed ? 1 : 0;
