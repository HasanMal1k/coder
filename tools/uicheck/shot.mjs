// Screenshots of the app, signed in as the account seed.mjs made.
//
//   node shot.mjs <folder> <name>=<path>[@<width>x<height>][!<action>]...
//
// A path is an address in the app, or "chat:<start of a seeded chat's title>",
// or "machine:<a seeded machine's name>". Actions run in order after the page
// has loaded: click:<text>, role:<role>~<name>, key:<key>, type:<text>,
// tab:<times>, hover:<text>, wait:<ms>; "full" takes the whole page, "2x"
// takes it at twice the detail, and "signedout" opens it without the
// account. For example:
//
//   node shot.mjs shots new=/ built=chat:Crypto phone=chat:Crypto@390x844 \
//     "dashboard=chat:Crypto!role:tab~Dashboard" "login=/login!signedout"
import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { BASE, seeded } from "./common.mjs";

const seed = seeded();
const [outDir, ...targets] = process.argv.slice(2);
mkdirSync(outDir, { recursive: true });

function resolve(path) {
  if (path.startsWith("chat:")) {
    const row = seed.chats.find((c) => c.split("|")[1].toLowerCase().startsWith(path.slice(5).toLowerCase()));
    if (!row) throw new Error("no chat " + path);
    return "/chats/" + row.split("|")[0];
  }
  if (path.startsWith("machine:")) return "/instances/" + seed.machines[path.slice(8)].id;
  return path;
}

const browser = await chromium.launch();
for (const target of targets) {
  const [name, rest] = target.split("=");
  const [spec, ...actions] = rest.split("!");
  const [path, size] = spec.split("@");
  const [width, height] = (size ?? "1440x900").split("x").map(Number);
  const signedOut = actions.includes("signedout");

  const context = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: actions.includes("2x") ? 2 : 1,
  });
  if (!signedOut) {
    await context.addInitScript((session) => {
      localStorage.setItem("coder.session", JSON.stringify(session));
    }, seed.session);
  }
  const page = await context.newPage();
  const errors = [];
  page.on("console", (m) => m.type() === "error" && errors.push(m.text().slice(0, 200)));
  page.on("pageerror", (e) => errors.push("pageerror: " + String(e).slice(0, 200)));

  await page.goto(BASE + resolve(path), { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(2500);

  for (const action of actions) {
    if (action === "signedout" || action === "2x") continue;
    const [verb, arg] = action.split(":");
    if (verb === "click") await page.getByText(arg, { exact: false }).first().click();
    else if (verb === "role") {
      const [role, label] = arg.split("~");
      await page.getByRole(role, { name: label }).first().click();
    } else if (verb === "key") await page.keyboard.press(arg);
    else if (verb === "type") await page.keyboard.type(arg);
    else if (verb === "tab") for (let i = 0; i < Number(arg); i++) await page.keyboard.press("Tab");
    else if (verb === "hover") await page.getByText(arg, { exact: false }).first().hover();
    else if (verb === "wait") await page.waitForTimeout(Number(arg));
    else if (verb === "full") continue;
    await page.waitForTimeout(700);
  }

  await page.screenshot({ path: `${outDir}/${name}.png`, fullPage: actions.includes("full") });
  console.log(name, errors.length ? "console errors: " + JSON.stringify(errors.slice(0, 4)) : "ok");
  await context.close();
}
await browser.close();
