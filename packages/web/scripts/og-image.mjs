// Draws the public site's social card, public/images/og-memax.png
// (1200×630), from the Ledger tokens, fonts and lockup in Chromium: the
// app has no next/og (it runs on Cloudflare Workers), and the card is
// never a handoff board. Run it after changing the card:
//
//   node packages/web/scripts/og-image.mjs
//
// It needs Playwright's Chromium (pnpm --filter @memaxlabs/web exec
// playwright install chromium).
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "@playwright/test";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const tokensDir = path.join(webRoot, "node_modules/@memaxlabs/ledger-tokens");
const out = path.join(webRoot, "public/images/og-memax.png");

const tokens = pathToFileURL(path.join(tokensDir, "tokens.css")).href;
const lockup = readFileSync(
  path.join(tokensDir, "assets/logos/memax-lockup-ink.svg"),
  "utf8",
)
  // The lockup's own fill is ink; draw it in the token so the card stays
  // the theme's.
  .replace(/fill="#[0-9a-f]{6}"/i, 'fill="currentColor"');

const html = `<!doctype html>
<html lang="en" data-theme="light">
<head>
<meta charset="utf-8">
<link rel="stylesheet" href="${tokens}">
<style>
  html, body { margin: 0; }
  body {
    width: 1200px; height: 630px; box-sizing: border-box;
    padding: 64px 72px 56px;
    display: flex; flex-direction: column;
    background: var(--paper); color: var(--ink);
    font-family: "Schibsted Grotesk", sans-serif;
  }
  .lockup { height: 40px; color: var(--ink); }
  .lockup svg { height: 40px; width: auto; display: block; }
  h1 {
    margin: 56px 0 0;
    font-family: "Newsreader", serif; font-weight: 360;
    font-size: 104px; line-height: 0.98; letter-spacing: -0.035em;
  }
  .row {
    margin-top: auto;
    display: grid; grid-template-columns: 1fr 260px; align-items: center;
    border-top: 1px solid var(--line); border-bottom: 1px solid var(--line);
  }
  .statement {
    padding: 22px 0;
    font-family: "Newsreader", serif; font-size: 28px; line-height: 36px;
  }
  .rail {
    align-self: stretch; display: flex; align-items: center; gap: 12px;
    padding-left: 20px; border-left: 1px solid var(--line);
    font-family: "IBM Plex Mono", monospace; font-size: 17px; line-height: 24px;
    color: var(--ink-2);
  }
  .stamp {
    display: inline-flex; align-items: center; justify-content: center;
    width: 34px; height: 24px; border-radius: 3px;
    background: var(--ink); color: var(--on-ink);
    font-family: "IBM Plex Mono", monospace; font-size: 13px; font-weight: 500;
  }
  .foot {
    display: flex; justify-content: space-between; margin-top: 20px;
    font-size: 20px; line-height: 28px; color: var(--ink-3);
  }
</style>
</head>
<body>
  <div class="lockup">${lockup}</div>
  <h1>One context.<br>Every agent.</h1>
  <div class="row">
    <div class="statement">MCP write tools must ask for confirmation with input_required.</div>
    <div class="rail"><span class="stamp">ZZ</span><span>kept · M-0430</span></div>
  </div>
  <div class="foot"><span>Nothing is kept without a receipt.</span><span>memax.app</span></div>
</body>
</html>`;

const browser = await chromium.launch();
const page = await browser.newPage({
  viewport: { width: 1200, height: 630 },
  deviceScaleFactor: 1,
});
// A file page, so the tokens and their fonts load from file URLs.
const dir = mkdtempSync(path.join(tmpdir(), "memax-og-"));
const file = path.join(dir, "card.html");
writeFileSync(file, html);
try {
  await page.goto(pathToFileURL(file).href, { waitUntil: "load" });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: out, type: "png" });
} finally {
  await browser.close();
  rmSync(dir, { recursive: true, force: true });
}
console.log(`wrote ${path.relative(process.cwd(), out)}`);
