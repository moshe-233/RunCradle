"use strict";
// Real UI regressions for the 2026-10-28 design; no engine or API doubles.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { spawn, execFileSync } = require("node:child_process");
const root = path.resolve(process.env.RUNCRADLE_WORK_DIR || ".work");
const cache = path.resolve(process.env.RUNCRADLE_CACHE_DIR || ".cache");
const { chromium } = require(
  require.resolve("playwright", {
    paths: [
      process.env.RUNCRADLE_PLAYWRIGHT_MODULE_DIR ||
        path.join(cache, "web-test/node_modules"),
    ],
  }),
);
const phase = process.argv[2] || "all";
const screenshotRoot =
  process.env.RUNCRADLE_SCREENSHOTS || "/tmp/runcradle-frontend";
fs.mkdirSync(screenshotRoot, { recursive: true });
fs.mkdirSync(path.join(root, "tmp"), { recursive: true });
const dir = fs.mkdtempSync(path.join(root, "tmp/frontend-"));
const config = { version: "0.5", processes: {} };
for (const [name, command, deps] of [
  ["prepare", "mkdir -p .work", []],
  ["build", "go build ./cmd/runcradle", ["prepare"]],
  ["lint", "go vet ./...", ["prepare"]],
  ["server", "echo server", ["build"]],
  ["test", "go test ./...", ["build"]],
  ["check", "python3 check.py", ["server", "test"]],
])
  config.processes[name] = {
    command,
    environment: name === "build" ? ["GOFLAGS=-trimpath"] : [],
    depends_on: Object.fromEntries(
      deps.map((n) => [n, { condition: "process_completed_successfully" }]),
    ),
    availability: { exit_on_end: true },
  };
config.processes.server.readiness_probe = {
  exec: { command: "echo ready" },
  period_seconds: 1,
};
fs.writeFileSync(
  path.join(dir, "process-compose.json"),
  JSON.stringify(config),
);
fs.writeFileSync(
  path.join(dir, "ui.json"),
  JSON.stringify({
    version: 1,
    title: "本地构建与检查",
    nodes: Object.fromEntries(
      Object.entries({
        prepare: "准备环境",
        build: "构建",
        lint: "检查代码",
        server: "启动服务",
        test: "单元测试",
        check: "结果检查",
      }).map(([k, label]) => [k, { label }]),
    ),
  }),
);
const server = spawn(
  path.join(root, "bin/runcradle"),
  [
    "serve",
    "--workspace",
    dir,
    "--process-compose",
    path.join(cache, "process-compose/v1.122.0/process-compose"),
  ],
  { stdio: ["ignore", "pipe", "pipe"] },
);
async function main() {
  let browser;
  try {
    const ready = await new Promise((resolve, reject) => {
      let output = "";
      const timer = setTimeout(
        () => reject(new Error("host startup timeout")),
        6000,
      );
      server.stdout.on("data", (b) => {
        output += b;
        if (output.includes("\n")) {
          clearTimeout(timer);
          resolve(JSON.parse(output.split("\n")[0]));
        }
      });
      server.once("exit", () => {
        clearTimeout(timer);
        reject(new Error("host exited before ready"));
      });
    });
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.RUNCRADLE_BROWSER ||
        path.join(cache, "web-test/chrome/chrome-linux64/chrome"),
      env: {
        ...process.env,
        XDG_CONFIG_HOME: path.join(cache, "xdg-config"),
        XDG_CACHE_HOME: path.join(cache, "xdg-cache"),
      },
    });
    const page = await browser.newPage({
      viewport: { width: 1585, height: 992 },
    });
    page.setDefaultTimeout(8000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (message) => {
      if (["error", "warning"].includes(message.type()))
        errors.push(message.text());
    });
    await page.goto(ready.pairing_url);
    await page.locator("#editor-graph .graph-node").waitFor();
    await page.locator("#open-import").click();
    await page.locator("#import-file-path").fill("process-compose.json");
    await page.locator("#import-ui-path").fill("ui.json");
    await page.locator("#import-workspace").click();
    await page.locator('#editor-graph [aria-label="构建"]').waitFor();
    const select = (name) =>
      page.locator(`#editor-graph .graph-node[aria-label="${name}"]`).click();
    if (["all", "keyboard"].includes(phase)) {
      await page.setViewportSize({ width: 390, height: 844 });
      const first = page.locator("#editor-graph .graph-node").first();
      await first.focus();
      await first.press("Enter");
      assert.equal(
        await page
          .locator("#inspector")
          .evaluate((el) => el.contains(document.activeElement)),
        true,
        "narrow inspector must receive keyboard focus",
      );
      await page.keyboard.press("Shift+Tab");
      assert.equal(
        await page
          .locator("#inspector")
          .evaluate((el) => el.contains(document.activeElement)),
        true,
        "Tab must not enter covered canvas controls",
      );
      await page.keyboard.press("Escape");
      assert.equal(await page.locator("#inspector").isVisible(), false);
      assert.equal(
        await first.evaluate((el) => el === document.activeElement),
        true,
        "Esc returns focus to the graph node",
      );
      await first.press("Enter");
      await page.locator("#close-inspector").click();
      assert.equal(
        await first.evaluate((el) => el === document.activeElement),
        true,
        "close button returns focus to the graph node",
      );
      await page.setViewportSize({ width: 1585, height: 992 });
      if (!(await page.locator("#sidebar").isVisible()))
        await page.locator("#toggle-sidebar").click();
      console.log(
        "PASS keyboard: overlay focus entry, Tab containment, Esc and close focus return",
      );
    }
    if (["all", "inspector"].includes(phase)) {
      await select("构建");
      await page.locator("#node-command").fill("echo unsaved input");
      await page.locator("#zoom-in").click();
      const zoom = await page
        .locator("#editor-graph")
        .getAttribute("data-zoom");
      await page.locator("#close-inspector").click();
      await select("构建");
      assert.equal(
        await page.locator("#node-command").inputValue(),
        "echo unsaved input",
        "closing inspector must preserve unapplied input",
      );
      await select("准备环境");
      await select("构建");
      assert.equal(
        await page.locator("#node-command").inputValue(),
        "echo unsaved input",
        "switching nodes must preserve unapplied input",
      );
      assert.equal(
        await page.locator("#editor-graph").getAttribute("data-zoom"),
        zoom,
      );
      await page.locator("#discard-node").click();
      assert.equal(
        await page.locator("#node-command").inputValue(),
        "go build ./cmd/runcradle",
      );
      await page.locator("#node-command").fill("echo applied");
      await page.locator("#node-label").fill("新的构建");
      await page.locator("#apply-node").click();
      await page.waitForFunction(() =>
        document.querySelector("#native-source").value.includes("echo applied"),
      );
      assert.equal(
        await page.locator("#node-label").inputValue(),
        "新的构建",
        "apply must include visible label edits",
      );
      await page.locator("#node-name").fill("compiled");
      await page.locator("#node-command").fill("echo renamed");
      await page.locator("#apply-node").click();
      await page.waitForFunction(() =>
        document.querySelector("#native-source").value.includes("echo renamed"),
      );
      assert.equal(
        await page.locator("#inspector-id").textContent(),
        "compiled",
        "apply must include the visible node name",
      );
      await page.locator("#node-name").fill("bound_build");
      await page
        .getByRole("button", { name: "绑定运行参数", exact: true })
        .first()
        .click();
      await page.locator("#bind-name").fill("build_flags");
      await page.locator("#confirm-bind").click();
      await page.waitForFunction(
        () => !document.querySelector("#bind-dialog").open,
      );
      assert.match(
        await page.locator("#ui-source").inputValue(),
        /bound_build/,
        "binding after rename must target the current process",
      );
      console.log(
        "PASS inspector: close/reopen, cross-node cache, explicit discard, apply label and command",
      );
    }
    if (["all", "visual"].includes(phase)) {
      if (phase === "all") {
        await page.locator("#close-inspector").click();
        await page.locator("#open-import").click();
        await page.locator("#import-file-path").fill("process-compose.json");
        await page.locator("#import-ui-path").fill("ui.json");
        await page.locator("#import-workspace").click();
      }
      const n = await page.locator("#inspector").isVisible();
      if (!n) await select("构建");
      await page.locator("#visual-style").selectOption("teal");
      await page.locator("#theme-toggle").click();
      assert.equal(
        await page.locator("body").getAttribute("data-palette"),
        "teal",
      );
      assert.equal(
        await page
          .locator("body")
          .evaluate((b) => getComputedStyle(b).colorScheme),
        "dark",
      );
      await page.locator("#save-workflow").click();
      await page.waitForFunction(() => location.search.includes("workflow="));
      await page.reload();
      await page.locator("#editor-graph .graph-node").first().waitFor();
      assert.equal(
        await page.locator("body").getAttribute("data-palette"),
        "teal",
        "theme survives reload",
      );
      assert.equal(
        await page
          .locator("body")
          .evaluate((b) => getComputedStyle(b).colorScheme),
        "dark",
      );
      await page.locator("#visual-style").selectOption("blue");
      await page.locator("#theme-toggle").click();
      await select("构建");
      await page.locator("#fit").click();
      for (const viewport of [
        { width: 1585, height: 992 },
        { width: 1024, height: 768 },
        { width: 390, height: 844 },
      ]) {
        await page.setViewportSize(viewport);
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
          true,
          "no horizontal page overflow",
        );
        await page.locator("#node-command").fill("echo responsive draft");
        await page.locator("#close-inspector").click();
        await select("构建");
        assert.equal(
          await page.locator("#node-command").inputValue(),
          "echo responsive draft",
        );
        await page.locator("#discard-node").click();
        await page.screenshot({
          path: path.join(screenshotRoot, `workbench-${viewport.width}.png`),
        });
      }
      await page.setViewportSize({ width: 1585, height: 992 });
      if (!(await page.locator("#sidebar").isVisible()))
        await page.locator("#toggle-sidebar").click();
      await page.locator("#discard-node").click();
      await page.locator("#theme-toggle").click();
      await page.screenshot({
        path: path.join(screenshotRoot, "workbench-dark.png"),
      });
      await page.locator("#visual-style").selectOption("teal");
      await page.screenshot({
        path: path.join(screenshotRoot, "workbench-teal-dark.png"),
      });
      await page.locator("#theme-toggle").click();
      await page.screenshot({
        path: path.join(screenshotRoot, "workbench-teal-light.png"),
      });
      await page.locator("#visual-style").selectOption("blue");
      await page.locator("#open-launchpad").click();
      assert.equal(
        await page.locator("#timeout-ms").isVisible(),
        false,
        "advanced runtime options start collapsed",
      );
      await page.screenshot({
        path: path.join(screenshotRoot, "launchpad.png"),
      });
      await page.keyboard.press("Escape");
      assert.equal(
        await page
          .locator("#open-launchpad")
          .evaluate((e) => e === document.activeElement),
        true,
      );
      await page.locator("#toggle-console").click();
      await page.locator("#evidence-details-tab").click();
      assert.equal(await page.locator("#run-facts").isVisible(), true);
      await page.locator("#evidence-overview-tab").click();
      assert.equal(await page.locator("#run-facts").isVisible(), false);
      console.log(
        "PASS visual: theme persistence, desktop/tablet/mobile, launchpad focus and advanced options, evidence tabs",
      );
    }
    if (["all", "offline"].includes(phase)) {
      const output = path.join(dir, "offline.html");
      execFileSync(path.join(root, "bin/runcradle"), [
        "workflow",
        "render",
        "--workspace",
        dir,
        "--file",
        "process-compose.json",
        "--ui",
        "ui.json",
        "--out",
        output,
      ]);
      const offline = await browser.newPage({
        viewport: { width: 1585, height: 992 },
      });
      const requests = [];
      offline.on("pageerror", (e) => errors.push(e.message));
      offline.on("request", (r) => {
        if (/^https?:/.test(r.url())) requests.push(r.url());
      });
      await offline.goto("file://" + output);
      const clipped = await offline.locator("#canvas").evaluate((canvas) => {
        const box = canvas.getBoundingClientRect();
        return [
          ...canvas.querySelectorAll(".graph-node > rect:first-of-type"),
        ].some((card) => {
          const r = card.getBoundingClientRect();
          return r.left < box.left || r.right > box.right;
        });
      });
      assert.equal(
        clipped,
        false,
        "initial offline view must fit the six-node fixture",
      );
      await offline.locator("#offline-palette").selectOption("teal");
      await offline.locator("#offline-theme").click();
      await offline.reload();
      assert.equal(
        await offline.locator("body").getAttribute("data-palette"),
        "teal",
      );
      assert.equal(
        await offline
          .locator("body")
          .evaluate((b) => getComputedStyle(b).colorScheme),
        "dark",
      );
      assert.equal(await offline.locator(".node-icon").count(), 6);
      await offline.locator("#offline-theme").click();
      await offline.screenshot({
        path: path.join(screenshotRoot, "offline.png"),
      });
      assert.deepEqual(requests, [], "offline export makes no HTTP requests");
      const overlaps = await offline.evaluate(() => {
        const payload = JSON.parse(
          document.querySelector("#workflow-data").textContent,
        );
        const id = payload.view.nodes.find((n) => n.name === "server").id;
        return ["not_ready", "unknown", "ready"].map((health) => {
          RunCradleGraph.draw(
            document.querySelector("#canvas"),
            payload,
            null,
            [{ name: "server", state: "running", health, exit_code: null }],
          );
          const card = document.querySelector(
            `#canvas .graph-node[data-node="${id}"]`,
          );
          const a = card.querySelector(".node-probe").getBoundingClientRect();
          const b = card.querySelector(".node-exit").getBoundingClientRect();
          return (
            a.left < b.right &&
            b.left < a.right &&
            a.top < b.bottom &&
            b.top < a.bottom
          );
        });
      });
      assert.deepEqual(
        overlaps,
        [false, false, false],
        "probe state and receipt remain independently readable",
      );
      await offline.close();
      const isolated = await browser.newPage();
      await isolated.addInitScript(() =>
        Object.defineProperty(window, "localStorage", {
          get() {
            throw new Error("storage denied");
          },
        }),
      );
      await isolated.goto("file://" + output);
      assert.equal(
        await isolated.locator(".graph-node").count(),
        6,
        "export works with denied storage",
      );
      await isolated.close();
      console.log(
        "PASS offline: shared theme persistence, icons, no HTTP, denied-storage fallback",
      );
    }
    assert.deepEqual(errors, [], "no browser runtime errors");
  } finally {
    await browser?.close();
    if (server.exitCode === null) {
      server.kill("SIGTERM");
      await new Promise((resolve) => server.once("exit", resolve));
    }
    fs.rmSync(dir, { recursive: true, force: true });
  }
}
main().catch((e) => {
  console.error(e.stack);
  process.exitCode = 1;
});
