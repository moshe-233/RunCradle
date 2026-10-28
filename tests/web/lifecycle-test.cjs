"use strict";
// 真正恢复同一文档，检查实际 SVG 尺寸；合成 pagehide 无法覆盖 BFCache。
const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");
const cache = path.resolve(process.env.RUNCRADLE_CACHE_DIR || ".cache");
const { chromium } = require(require.resolve("playwright", {
  paths: [process.env.RUNCRADLE_PLAYWRIGHT_MODULE_DIR || path.join(cache, "web-test/node_modules")],
}));
const assets = path.resolve("internal/webui/assets");
const markup = fs.readFileSync(path.join(assets, "shell.html"), "utf8")
  .replace(/<script[^>]*><\/script>/g, "")
  .replace("</body>", `<script src="/theme.js"></script><script src="/graph.js"></script>
<script type="module">
import { createView } from "/workbench/view.js";
window.restored = false;
window.documentMarker = crypto.randomUUID();
window.addEventListener("pageshow", event => { window.restored = event.persisted; });
const display = { graph: { width: 2400, height: 800, nodes: [], edges: [] }, view: { nodes: [] } };
const noop = () => {};
const view = createView({
  viewState: { mode: "design", direction: "", editorBusy: false, disabledBefore: [] },
  addNode: noop, closeInspector: noop, inspectorOverlay: () => false,
  inspectorPresentation: noop, launch: noop, readEditor: () => ({ document: { display } }),
  readInspector: () => ({}), readRuns: () => ({}), rememberInspector: noop,
  selectWorkbenchNode: noop, syncButtons: noop,
});
const canvas = document.getElementById("editor-graph");
RunCradleGraph.draw(canvas, display);
canvas._display = display;
view.bindEvents();
window.ready = true;
</script></body>`);
const routes = new Map([
  ["/theme.js", "theme.js"], ["/graph.js", "graph.js"],
  ["/workbench/view.js", "workbench/view.js"],
]);
const server = http.createServer((request, response) => {
  if (request.url === "/workbench") {
    response.setHeader("Content-Type", "text/html; charset=utf-8");
    response.end(markup);
  } else if (request.url === "/next") {
    response.end("<title>Next</title>Next");
  } else if (request.url === "/app.css") {
    response.setHeader("Content-Type", "text/css");
    response.end(["theme.css", "shell.css", "graph.css"].map(name =>
      fs.readFileSync(path.join(assets, name), "utf8")).join("\n"));
  } else if (routes.has(request.url)) {
    response.setHeader("Content-Type", "application/javascript");
    response.end(fs.readFileSync(path.join(assets, routes.get(request.url))));
  } else response.writeHead(204).end();
});

async function main() {
  let browser;
  try {
    await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
    browser = await chromium.launch({
      headless: true,
      // Playwright 默认禁用 BFCache；此测试明确启用真实历史恢复。
      ignoreDefaultArgs: ["--disable-back-forward-cache"],
      executablePath: process.env.RUNCRADLE_BROWSER || path.join(cache, "web-test/chrome/chrome-linux64/chrome"),
      env: { ...process.env, XDG_CONFIG_HOME: path.join(cache, "xdg-config"), XDG_CACHE_HOME: path.join(cache, "xdg-cache") },
    });
    const page = await browser.newPage({ viewport: { width: 1585, height: 992 } });
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    const base = `http://127.0.0.1:${server.address().port}`;
    await page.goto(base + "/workbench");
    await page.waitForFunction(() => window.ready && Number(document.querySelector("#editor-graph svg").getAttribute("width")) < 2400);
    const marker = await page.evaluate(() => window.documentMarker);
    await page.goto(base + "/next");
    await page.goBack({ waitUntil: "commit" });
    await page.waitForFunction(() => window.ready && window.restored);
    assert.equal(await page.evaluate(() => window.documentMarker), marker, "same document restored from BFCache");
    await page.setViewportSize({ width: 1000, height: 992 });
    await page.waitForFunction(() => {
      const canvas = document.getElementById("editor-graph");
      return Number(canvas.querySelector("svg").getAttribute("width")) <= canvas.clientWidth;
    }, null, { timeout: 2000 });
    assert.deepEqual(errors, [], "no lifecycle runtime errors");
    console.log("PASS lifecycle: BFCache restores the same document and canvas auto-fit survives resize");
  } finally {
    await browser?.close();
    await new Promise(resolve => server.close(resolve));
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
