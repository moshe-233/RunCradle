"use strict";
// Browser regressions; deterministic API fixtures cover failure and race paths.
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
  ["build", "sleep 2; echo build", ["prepare"]],
  ["lint", "echo lint", ["prepare"]],
  ["server", "echo server", ["build"]],
  ["test", "echo test", ["build"]],
  ["check", "echo check", ["server", "test"]],
])
  config.processes[name] = {
    command,
    environment: name === "build" ? ["GOFLAGS=-trimpath"] : [],
    depends_on: Object.fromEntries(
      deps.map((n) => [n, { condition: "process_completed_successfully" }]),
    ),
    availability: { restart: "no" },
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
  process.env.RUNCRADLE_TEST_BINARY || path.join(root, "bin/runcradle"),
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
  let browser, video;
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
      recordVideo: process.env.RUNCRADLE_USABILITY_VIDEO
        ? {
            dir: process.env.RUNCRADLE_USABILITY_VIDEO,
            size: { width: 1585, height: 992 },
          }
        : undefined,
      viewport: { width: 1585, height: 992 },
      reducedMotion: process.env.RUNCRADLE_REDUCED_MOTION
        ? "reduce"
        : "no-preference",
    });
    video = page.video();
    page.setDefaultTimeout(8000);
    const errors = [];
    let expectingValidationFailure = false;
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (message) => {
      if (
        expectingValidationFailure &&
        message.text() ===
          "Failed to load resource: the server responded with a status of 400 (Bad Request)"
      )
        return;
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
    await page.locator("#save-workflow").click();
    await page.waitForFunction(() => location.search.includes("workflow="));

    const originalURL = page.url();
    const run = async (name, fn) => {
      if (phase !== "all" && phase !== name) return;
      await page.goto(originalURL);
      await page.locator('#editor-graph [aria-label="构建"]').waitFor();
      await page.waitForFunction(
        () => !document.querySelector("#new-workflow").disabled,
      );
      await fn();
      console.log("PASS usability: " + name);
    };
    await run("compact-records", async () => {
      await page.evaluate(async () => {
        const { createRuns } = await import("/workbench/runs.js");
        const noop = () => {};
        const runs = createRuns({
          runsState: {},
          readEditor: () => ({}),
          guard: (fn) => fn,
          notice: noop,
          api: async () => ({
            runs: [
              {
                run_id: "1234567890abcdef",
                target_nodes: ["build"],
                task_result: "succeeded",
                reason: "targets_terminal",
                started_at: "2026-10-31T03:15:40Z",
              },
            ],
            has_more: false,
            next_offset: 1,
          }),
        });
        await runs.runHistory();
      });
      for (const host of ["workflow-list", "run-history"]) {
        if (host === "run-history") await page.locator("#history-tab").click();
        const row = page.locator(`#${host} .resource-row`).first();
        const copy = row.locator(".record-copy");
        await page.mouse.move(800, 500);
        assert.equal(
          await copy.evaluate((el) => getComputedStyle(el).opacity),
          "0",
          "copy hidden at rest",
        );
        assert.match(
          await row.locator("small").textContent(),
          /^\d{1,2}\/\d{1,2} \d{2}:\d{2}$/,
        );
        assert.equal(
          (await row.textContent()).includes("targets_terminal"),
          false,
        );
        assert.ok((await row.boundingBox()).height <= 64);
        await row.hover();
        await page.waitForFunction(
          (element) => getComputedStyle(element).opacity === "1",
          await copy.elementHandle(),
        );
        if (process.env.RUNCRADLE_COMPACT_SCREENSHOTS) {
          for (const theme of ["blue-light", "graphite-amber"]) {
            await page.locator("#visual-style").selectOption(theme);
            await row.screenshot({
              path: path.join(screenshotRoot, `compact-${host}-${theme}.png`),
            });
          }
        }
        const bounds = await row.evaluate((el) => ({
          title: el.querySelector("strong").getBoundingClientRect().toJSON(),
          copy: el
            .querySelector(".record-copy")
            .getBoundingClientRect()
            .toJSON(),
        }));
        assert.ok(
          Math.abs(bounds.title.y - bounds.copy.y) < 15,
          "copy aligns with title",
        );
        await page.evaluate(() => {
          window.copiedID = "";
          Object.defineProperty(navigator, "clipboard", {
            configurable: true,
            value: {
              writeText: async (value) => {
                window.copiedID = value;
              },
            },
          });
        });
        const id = await row.getAttribute("data-record");
        await copy.click();
        assert.equal(await page.evaluate(() => window.copiedID), id);
        await page.mouse.move(800, 500);
        await row.locator(".record-select").focus();
        assert.equal(
          await copy.evaluate((el) => getComputedStyle(el).opacity),
          "1",
          "keyboard focus reveals copy",
        );
      }
      const touch = await browser.newContext({
        viewport: { width: 390, height: 844 },
        hasTouch: true,
        isMobile: true,
        storageState: await page.context().storageState(),
      });
      const touchPage = await touch.newPage();
      await touchPage.goto(originalURL);
      await touchPage.locator("#editor-graph .graph-node").first().waitFor();
      await touchPage.locator("#toggle-sidebar").click();
      const touchCopy = touchPage
        .locator("#workflow-list .record-copy")
        .first();
      await touchCopy.waitFor({ state: "visible" });
      assert.equal(
        await touchCopy.evaluate((el) => getComputedStyle(el).opacity),
        "1",
        "touch devices show copy",
      );
      await touchPage.mouse.move(380, 820);
      await touchPage.waitForTimeout(350);
      if (process.env.RUNCRADLE_COMPACT_SCREENSHOTS)
        await touchPage.screenshot({
          path: path.join(screenshotRoot, "compact-mobile.png"),
        });
      await touch.close();
    });
    await run("operation", async () => {
      let release,
        writes = 0;
      const held = new Promise((resolve) => {
        release = resolve;
      });
      await page.route("**/api/v1/workflows/*", async (route) => {
        if (route.request().method() !== "PUT") return route.continue();
        writes++;
        await held;
        await route.continue();
      });
      await page.locator("#save-workflow").click();
      await page.waitForFunction(() =>
        document.querySelector("#save-workflow").textContent.includes("保存中"),
      );
      assert.equal(await page.locator("#save-workflow").isDisabled(), true);
      await page.waitForFunction(() =>
        document
          .querySelector("#save-workflow")
          .classList.contains("operation-spinning"),
      );
      await page.locator("#save-workflow").evaluate((el) => el.click());
      release();
      await page.waitForFunction(
        () => document.querySelector("#save-workflow").textContent === "已保存",
      );
      assert.equal(writes, 1);
      await page.unroute("**/api/v1/workflows/*");
      await page.waitForFunction(
        () =>
          document.querySelector("#save-workflow").textContent === "保存版本",
      );
    });
    await run("request-boundaries", async () => {
      const result = await page.evaluate(async () => {
        const { withOperation, fieldErrors } = await import(
          "/workbench/feedback.js"
        );
        const button = document.querySelector("#save-workflow");
        const before = button.getBoundingClientRect().width;
        let release;
        const held = new Promise((resolve) => {
          release = resolve;
        });
        const pending = withOperation(
          "save-workflow",
          "保存中…",
          async (success) => {
            await held;
            success("已保存");
          },
        );
        await new Promise((resolve) => setTimeout(resolve, 180));
        const during = button.getBoundingClientRect().width;
        release();
        await pending;
        const after = button.getBoundingClientRect().width;
        return { before, during, after };
      });
      assert.ok(
        Math.abs(result.before - result.during) < 1,
        JSON.stringify(result),
      );
      assert.ok(
        Math.abs(result.before - result.after) < 1,
        JSON.stringify(result),
      );
      await select("构建");
      await page.locator("#add-env").click();
      const key = page.locator("#env-rows .env-key").last();
      await key.fill("BAD=KEY");
      await page.locator("#apply-node").click();
      await page.waitForFunction(() =>
        document.querySelector("#env-rows .field-error"),
      );
      const layout = await key.evaluate((field) => {
        const row = field.closest(".kv-row"),
          error = row.querySelector(".field-error");
        return {
          field: field.getBoundingClientRect().toJSON(),
          error: error.getBoundingClientRect().toJSON(),
        };
      });
      assert.ok(layout.error.y >= layout.field.bottom, JSON.stringify(layout));
      await page.locator("#discard-node").click();
      await page.locator("#node-command").fill("echo retained");
      let release;
      const held = new Promise((resolve) => {
        release = resolve;
      });
      expectingValidationFailure = true;
      await page.route("**/api/v1/validate", async (route) => {
        await held;
        await route.fulfill({
          status: 400,
          json: { error: "delayed node failure" },
        });
      });
      await page.locator("#apply-node").click();
      await select("检查代码");
      release();
      await page.waitForFunction(
        () => !document.querySelector("#apply-node").disabled,
      );
      assert.equal(await page.locator("#inspector-error").isVisible(), false);
      await page.unroute("**/api/v1/validate");
      expectingValidationFailure = false;
      await select("构建");
      assert.match(
        await page.locator("#inspector-error").textContent(),
        /delayed node failure/,
      );
      assert.equal(
        await page.locator("#node-command").inputValue(),
        "echo retained",
      );
    });
    await run("help", async () => {
      await page.locator("#fit").focus();
      await page.locator('[role="tooltip"]').waitFor({ state: "visible" });
      assert.match(await page.locator('[role="tooltip"]').textContent(), /0/);
      assert.ok(await page.locator("#fit").getAttribute("aria-describedby"));
      await page.keyboard.press("Escape");
      await page.locator('[role="tooltip"]').waitFor({ state: "hidden" });
      await page.locator("#more-actions summary").click();
      await page.locator("#open-help").click();
      assert.equal(
        await page.locator("#help-dialog").evaluate((e) => e.open),
        true,
      );
      assert.match(await page.locator("#help-dialog").textContent(), /Ctrl/);
      await page.keyboard.press("Escape");
      await page.waitForFunction(
        () =>
          document.activeElement ===
          document.querySelector("#more-actions summary"),
      );
      await page.locator("#open-launchpad").click();
      await page.locator(".launch-advanced summary").click();
      await page.locator("#retain-evidence").waitFor({ state: "visible" });
      await page.locator("#retain-evidence").focus();
      await page.locator('[role="tooltip"]').waitFor({ state: "visible" });
      assert.equal(
        await page.locator('[role="tooltip"]').evaluate((tip) => {
          const box = tip.getBoundingClientRect();
          return (
            !!tip.closest("dialog[open]") &&
            box.left >= 0 &&
            box.right <= innerWidth &&
            box.top >= 0 &&
            box.bottom <= innerHeight
          );
        }),
        true,
      );
      await page.keyboard.press("Escape");
      assert.equal(
        await page.locator("#launchpad").evaluate((dialog) => dialog.open),
        true,
      );
      await page.keyboard.press("Escape");
    });
    await run("read-races", async () => {
      const result = await page.evaluate(async () => {
        const { createRuns } = await import("/workbench/runs.js");
        const pending = [],
          state = {},
          noop = () => {};
        let workflow = { workflow_id: "one" };
        const runs = createRuns({
          runsState: state,
          api: () =>
            new Promise((resolve, reject) => pending.push({ resolve, reject })),
          readEditor: () => ({ workflow }),
          guard: (fn) => fn,
          notice: noop,
        });
        const first = runs.runHistory();
        const second = runs.runHistory();
        pending[1].resolve({
          runs: [
            { run_id: "new", target_nodes: ["build"], task_result: "pending" },
          ],
          has_more: true,
          next_offset: 20,
        });
        await second;
        pending[0].reject(new Error("obsolete"));
        let staleRaised = false;
        try {
          await first;
        } catch {
          staleRaised = true;
        }
        const retained = document.querySelector("#run-history").textContent;
        workflow = { workflow_id: "two" };
        const third = runs.runHistory();
        const cleared = !document.querySelector("#run-history .resource-row");
        pending[2].reject(new Error("current failure"));
        try {
          await third;
        } catch {}
        return {
          staleRaised,
          retained,
          cleared,
          error: document.querySelector("#run-history-state").textContent,
        };
      });
      assert.equal(result.staleRaised, false);
      assert.match(result.retained, /build/);
      assert.equal(result.cleared, true);
      assert.match(result.error, /current failure/);
    });
    await run("terminal-snapshot-race", async () => {
      const result = await page.evaluate(async () => {
        const { createRuns } = await import("/workbench/runs.js");
        const original = window.EventSource,
          streams = [],
          state = {},
          noop = () => {};
        let releaseSnapshot, refreshStarted;
        let reads = 0;
        const refreshing = new Promise((resolve) => (refreshStarted = resolve));
        const pending = {
          run_id: "terminal-race",
          owned: true,
          final: false,
          nodes: [],
          target_nodes: ["build"],
          task_result: "pending",
        };
        window.EventSource = class {
          constructor() {
            this.listeners = {};
            streams.push(this);
          }
          addEventListener(name, handler) {
            this.listeners[name] = handler;
          }
          close() { this.closed = true; }
        };
        try {
          const runs = createRuns({
            runsState: state,
            api: async (resource) => {
              if (resource.endsWith("/definition"))
                return { source: "fixture", display: { view: { nodes: [] } } };
              if (resource.startsWith("runs?")) return { runs: [] };
              if (resource === "runs/next-run")
                return { ...pending, run_id: "next-run" };
              if (++reads === 1) return pending;
              return new Promise((resolve) => {
                releaseSnapshot = resolve;
                refreshStarted();
              });
            },
            readEditor: () => ({}),
            readView: () => ({ mode: "monitor" }),
            drawGraph: noop,
            renderNodeFacts: noop,
            setMode: noop,
            resetCanvas: noop,
            notice: noop,
            guard: (fn) => fn,
          });
          await runs.openRun(pending.run_id);
          const stream = streams[0];
          stream.onmessage({ data: JSON.stringify({ seq: 1, kind: "state" }) });
          await refreshing;
          stream.listeners.final({
            data: JSON.stringify({
              ...pending,
              final: true,
              task_result: "succeeded",
              cleanup: "complete",
              history_integrity: "complete",
            }),
          });
          // The HTTP refresh was sampled before the final SSE, but arrives after it.
          releaseSnapshot(pending);
          await new Promise(requestAnimationFrame);
          const result = {
            status: document.querySelector("#run-status").textContent,
            final: state.snapshot.final,
            stopped: document.querySelector("#stop-run").disabled,
            closed: stream.closed,
          };
          await runs.openRun("next-run");
          return {
            ...result,
            nextStatus: document.querySelector("#run-status").textContent,
            nextID: state.snapshot.run_id,
          };
        } finally {
          window.EventSource = original;
        }
      });
      assert.deepEqual(
        result,
        {
          status: "succeeded", final: true, stopped: true, closed: true,
          nextStatus: "pending", nextID: "next-run",
        },
        "a late refresh must preserve finality without blocking the next run",
      );
    });
    await run("event-memory", async () => {
      const result = await page.evaluate(async () => {
        const { createRuns } = await import("/workbench/runs.js");
        const { eventFilters } = await import("/workbench/feedback.js");
        const original = window.EventSource,
          streams = [],
          state = {},
          noop = () => {};
        window.EventSource = class {
          constructor(url) {
            this.url = url;
            streams.push(this);
          }
          addEventListener() {}
          close() {}
        };
        try {
          const runs = createRuns({
            runsState: state,
            api: async (path) =>
              path.endsWith("/definition")
                ? { source: "fixture", display: { view: { nodes: [] } } }
                : path.startsWith("runs?")
                  ? { runs: [] }
                  : {
                      run_id: path.split("/")[1],
                      owned: true,
                      final: false,
                      nodes: [],
                      task_result: "pending",
                    },
            readEditor: () => ({}),
            readView: () => ({ mode: "monitor" }),
            drawGraph: noop,
            renderNodeFacts: noop,
            setMode: noop,
            resetCanvas: noop,
            notice: noop,
            guard: (fn) => fn,
          });
          await runs.openRun("one");
          const emit = (stream, seq) =>
            stream.onmessage({
              lastEventId: String(seq),
              data: JSON.stringify({
                seq,
                kind: "state",
                node: { name: "build", state: "running" },
              }),
            });
          emit(streams.at(-1), 1);
          document.querySelector("#event-filter").value = "no-match";
          eventFilters();
          await runs.openRun("one");
          const cursor = streams.at(-1).url;
          emit(streams.at(-1), 1);
          emit(streams.at(-1), 2);
          const same = {
            count: document.querySelector("#timeline").children.length,
            query: document.querySelector("#event-filter").value,
            visible: [...document.querySelector("#timeline").children].filter(
              (row) => !row.hidden,
            ).length,
          };
          await runs.openRun("two");
          return {
            cursor,
            same,
            reset: document.querySelector("#event-filter").value,
            count: document.querySelector("#timeline").children.length,
          };
        } finally {
          window.EventSource = original;
        }
      });
      assert.match(result.cursor, /after_seq=1/);
      assert.deepEqual(result.same, {
        count: 2,
        query: "no-match",
        visible: 0,
      });
      assert.equal(result.reset, "");
      assert.equal(result.count, 0);
    });
    await run("tooltip-fallback", async () => {
      await page.addInitScript(() => {
        delete HTMLElement.prototype.showPopover;
      });
      await page.goto(originalURL);
      await page.waitForFunction(
        () => !document.querySelector("#new-workflow").disabled,
      );
      await page.locator("#open-launchpad").click();
      await page.locator(".launch-advanced summary").click();
      await page.locator("#retain-evidence").waitFor({ state: "visible" });
      await page.locator("#retain-evidence").focus();
      await page.locator('[role="tooltip"]').waitFor({ state: "visible" });
      assert.equal(
        await page
          .locator('[role="tooltip"]')
          .evaluate(
            (tip) =>
              !tip.hasAttribute("popover") && !!tip.closest("dialog[open]"),
          ),
        true,
      );
      await page.keyboard.press("Escape");
      assert.equal(
        await page.locator("#launchpad").evaluate((dialog) => dialog.open),
        true,
      );
    });
    await run("outcomes", async () => {
      await page.route("**/api/v1/workflows?*", (route) =>
        route.fulfill({ status: 503, json: { error: "list unavailable" } }),
      );
      await page.locator("#save-workflow").click();
      await page.waitForFunction(
        () => document.querySelector("#workflow-list").dataset.readError,
      );
      assert.equal(
        await page.locator("#save-workflow").textContent(),
        "已保存",
      );
      assert.match(
        await page.locator("#notice").textContent(),
        /版本已保存；列表读取失败/,
      );
      await page.unroute("**/api/v1/workflows?*");
      await page.locator("#workflow-list-state button").click();
      await page.waitForFunction(
        () => !document.querySelector("#workflow-list").dataset.readError,
      );
      for (let index = errors.length - 1; index >= 0; index--)
        if (/503/.test(errors[index])) errors.splice(index, 1);
      let submissions = 0;
      await page.route("**/api/v1/runs", async (route) => {
        submissions++;
        await route.abort();
      });
      await page.route("**/api/v1/run-requests/*", (route) => route.abort());
      await page.locator("#open-launchpad").click();
      await page.locator('#run-targets input[value="build"]').check();
      await page.locator("#start-run").click();
      await page.waitForFunction(() =>
        document.querySelector("#notice").textContent.includes("提交结果未知"),
      );
      assert.equal(submissions, 1);
      assert.equal(
        await page
          .locator("#start-run")
          .evaluate((button) => button.classList.contains("operation-success")),
        false,
      );
      assert.equal(
        await page.locator("#launchpad").evaluate((dialog) => dialog.open),
        true,
      );
      await page.unroute("**/api/v1/runs");
      await page.unroute("**/api/v1/run-requests/*");
      for (let index = errors.length - 1; index >= 0; index--)
        if (/ERR_FAILED/.test(errors[index])) errors.splice(index, 1);
    });
    await run("stop-result", async () => {
      await page.locator("#open-launchpad").click();
      await page.locator('#run-targets input[value="build"]').check();
      await page.locator("#start-run").click();
      await page.waitForFunction(
        () => !document.querySelector("#stop-run").disabled,
      );
      await page.route("**/api/v1/runs/*/stop", (route) =>
        route.fulfill({
          json: { requested: false, terminal: true, cleanup: "complete" },
        }),
      );
      await page.locator("#toggle-console").click();
      await page.locator("#stop-run").click();
      await page.waitForFunction(() =>
        document.querySelector("#notice").textContent.includes("运行已结束"),
      );
      assert.equal(
        await page
          .locator("#stop-run")
          .evaluate((button) => button.classList.contains("operation-success")),
        false,
      );
      await page.unroute("**/api/v1/runs/*/stop");
    });
    await run("fields", async () => {
      await select("构建");
      await page.locator("#node-name").fill("prepare");
      await page.locator("#apply-node").click();
      await page.waitForFunction(
        () =>
          document.querySelector("#node-name").getAttribute("aria-invalid") ===
          "true",
      );
      assert.match(
        await page.locator("#inspector-error").textContent(),
        /重名|已存在/,
      );
      await page.waitForFunction(
        () => document.activeElement === document.querySelector("#node-name"),
      );
      await page.locator("#node-name").fill("build");
      assert.equal(
        await page.locator("#node-name").getAttribute("aria-invalid"),
        null,
      );
      await page.locator("#node-command").focus();
      await page.locator('[data-inspector-section="advanced"]').click();
      assert.equal(
        await page.locator("#node-advanced").evaluate((e) => e.open),
        true,
      );
      assert.equal(
        await page.evaluate(() => document.activeElement.id),
        "node-command",
      );
      await page.locator("#discard-node").click();
      await page.locator("#close-inspector").click();
      await page.locator("#open-launchpad").click();
      await page.locator('#run-targets input[value="build"]').check();
      await page.locator(".launch-advanced summary").click();
      await page.locator("#timeout-ms").fill("3600001");
      await page.locator(".launch-advanced summary").click();
      await page.locator("#start-run").click();
      await page.waitForFunction(
        () =>
          document.querySelector("#timeout-ms").getAttribute("aria-invalid") ===
          "true",
      );
      assert.equal(
        await page.locator(".launch-advanced").evaluate((e) => e.open),
        true,
      );
      await page.waitForFunction(
        () => document.activeElement === document.querySelector("#timeout-ms"),
      );
    });
    await run("evidence", async () => {
      await page.locator("#toggle-console").click();
      const result = await page.evaluate(async () => {
        const { createRuns } = await import("/workbench/runs.js");
        const state = { activeRun: "", snapshot: null, stream: null },
          noop = () => {};
        const snapshot = {
          run_id: "fixture-run",
          revision: "fixture-version",
          owned: false,
          final: true,
          task_result: "succeeded",
          target_nodes: ["a"],
          history_integrity: "gap",
          cleanup: "unknown",
          io_status: "partial",
          engine_exit_code: null,
          nodes: [
            {
              name: "a",
              state: "completed",
              health: "unhealthy",
              exit_code: null,
              source: "snapshot",
            },
            {
              name: "b",
              state: "failed",
              health: "unknown",
              exit_code: 7,
              source: "receipt",
            },
          ],
        };
        const runs = createRuns({
          runsState: state,
          api: async (path) =>
            path.endsWith("/definition")
              ? { source: "fixture", display: { view: { nodes: [] } } }
              : path.startsWith("runs?")
                ? { runs: [], has_more: false }
                : snapshot,
          readEditor: () => ({}),
          readView: () => ({ mode: "monitor" }),
          drawGraph: noop,
          renderNodeFacts: noop,
          setMode: noop,
          resetCanvas: noop,
          notice: noop,
          guard: (fn) => fn,
        });
        await runs.openRun("fixture-run");
        const row = document.querySelector('[data-fact-name="a"]');
        row.querySelector("details").open = true;
        row.querySelector("summary").focus();
        await runs.openRun("fixture-run");
        return {
          same: row === document.querySelector('[data-fact-name="a"]'),
          open: row.querySelector("details").open,
          focus: document.activeElement === row.querySelector("summary"),
          text: document.querySelector("#run-overview-summary").textContent,
          health: row.textContent,
        };
      });
      assert.equal(result.same, true);
      assert.equal(result.open, true);
      assert.equal(result.focus, true);
      assert.match(result.text, /成功/);
      assert.match(result.health, /unhealthy/);
      assert.match(result.health, /直接退出回执[\s\S]*未知/);
      assert.match(await page.locator("#run-integrity").textContent(), /gap/);
      assert.equal(await page.locator("#run-cleanup").textContent(), "unknown");
      await page.locator("#fact-filter").selectOption("failed");
      assert.equal(
        await page.locator("#run-nodes .node-fact:visible").count(),
        1,
      );
      assert.match(await page.locator("#fact-count").textContent(), /1.*2/);
    });
    await run("resource-filter", async () => {
      await page.locator("#library-filter").fill("no-match-usability");
      await page.locator("#new-workflow").click();
      await page.locator("#save-workflow").click();
      await page.waitForFunction(
        () =>
          document.querySelectorAll("#workflow-list button[data-workflow]")
            .length > 1,
      );
      assert.equal(
        await page
          .locator("#workflow-list button[data-workflow]:visible")
          .count(),
        0,
        "new records reuse active query",
      );
      assert.match(
        await page.locator("#workflow-list-state").textContent(),
        /没有匹配/,
      );
      await page.locator("#clear-library-filter").click();
      assert.equal(await page.locator("#library-filter").inputValue(), "");
      assert.ok(
        (await page
          .locator("#workflow-list button[data-workflow]:visible")
          .count()) > 0,
      );
    });
    await run("event-filter", async () => {
      await page.locator("#open-launchpad").click();
      await page.locator('#run-targets input[value="build"]').check();
      await page.locator("#start-run").click();
      await page.waitForFunction(
        () => document.querySelector("#run-id").textContent,
      );
      await page.locator("#toggle-console").click();
      await page.locator("#evidence-logs-tab").click();
      await page.locator("#event-filter").fill("no-event-matches");
      await page.waitForFunction(
        () => document.querySelector("#run-status").textContent === "succeeded",
      );
      assert.equal(
        await page.locator("#timeline > li:visible").count(),
        0,
        "new events reuse active query",
      );
      assert.match(
        await page.locator("#event-list-state").textContent(),
        /没有匹配/,
      );
      await page.locator("#event-filter").fill("");
      assert.ok((await page.locator("#timeline > li:visible").count()) > 0);
      assert.match(await page.locator("#event-count").textContent(), /已缓存/);
    });
    if (phase === "visual" || phase.startsWith("capture-")) {
      await page.goto(originalURL);
      await page.waitForFunction(
        () => !document.querySelector("#new-workflow").disabled,
      );
      if (process.env.RUNCRADLE_THEME)
        await page
          .locator("#visual-style")
          .selectOption(process.env.RUNCRADLE_THEME);
      const shot = async (label) => {
        await page.waitForTimeout(320);
        await page.screenshot({
          path: path.join(screenshotRoot, `${label}.png`),
        });
      };
      if (phase === "visual") {
        const measurements = [];
        for (const viewport of [
          { width: 1585, height: 992 },
          { width: 1366, height: 768 },
          { width: 1024, height: 768 },
          { width: 390, height: 844 },
        ]) {
          await page.setViewportSize(viewport);
          for (const theme of ["blue-light", "teal-light", "graphite-amber"]) {
            await page.locator("#visual-style").selectOption(theme);
            await select("构建");
            await page.locator("#node-name").fill("prepare");
            await page.locator("#apply-node").click();
            await page.waitForFunction(
              () =>
                document
                  .querySelector("#node-name")
                  .getAttribute("aria-invalid") === "true",
            );
            const identity = `usability-${viewport.width}-${theme}`;
            await shot(identity + "-error");
            const measure = await page.evaluate(() => ({
              width: innerWidth,
              scrollWidth: document.documentElement.scrollWidth,
              font: parseFloat(
                getComputedStyle(document.querySelector("#node-name")).fontSize,
              ),
              error: document
                .querySelector(".field-error")
                .getBoundingClientRect()
                .toJSON(),
            }));
            assert.ok(measure.scrollWidth <= measure.width, identity);
            assert.ok(measure.font >= 12, identity);
            measurements.push({ identity, ...measure });
            await page.locator("#discard-node").click();
            await page.locator("#close-inspector").click();
            await page.locator("#open-launchpad").click();
            await page.locator('#run-targets input[value="build"]').check();
            await shot(identity + "-launch");
            await page.keyboard.press("Escape");
          }
        }
        fs.writeFileSync(
          path.join(screenshotRoot, "measurements.json"),
          JSON.stringify(measurements, null, 2),
        );
        await page.locator("#open-launchpad").click();
        await page.locator('#run-targets input[value="build"]').check();
        await page.locator("#start-run").click();
        await page.waitForFunction(
          () =>
            document.querySelector("#run-status").textContent === "succeeded",
        );
        await page.locator("#toggle-console").click();
        for (const viewport of [
          { width: 1585, height: 992 },
          { width: 1366, height: 768 },
          { width: 1024, height: 768 },
          { width: 390, height: 844 },
        ]) {
          await page.setViewportSize(viewport);
          for (const theme of ["blue-light", "teal-light", "graphite-amber"]) {
            await page.locator("#visual-style").selectOption(theme);
            await page.locator("#console-body").evaluate((body) => {
              body.scrollTop = 0;
            });
            await shot(`usability-${viewport.width}-${theme}-evidence`);
            assert.equal(
              await page.evaluate(
                () => document.documentElement.scrollWidth <= innerWidth,
              ),
              true,
            );
          }
        }
      } else {
        await page
          .context()
          .tracing.start({ screenshots: true, snapshots: true });
        await shot(phase + "-before");
        if (phase === "capture-operation") {
          await select("构建");
          await page.locator("#node-command").fill("sleep 2; echo updated");
          await shot(phase + "-pending");
          await page.locator("#apply-node").click();
          await page.waitForFunction(
            () =>
              document.querySelector("#apply-node").textContent === "已应用",
          );
          await page.locator("#close-inspector").click();
          let release;
          const held = new Promise((resolve) => {
            release = resolve;
          });
          await page.route("**/api/v1/workflows/*", async (route) => {
            if (route.request().method() === "PUT") await held;
            await route.continue();
          });
          await page.locator("#save-workflow").click();
          await page.waitForFunction(() =>
            document
              .querySelector("#save-workflow")
              .classList.contains("operation-spinning"),
          );
          await shot(phase + "-saving");
          release();
          await page.waitForFunction(
            () =>
              document.querySelector("#save-workflow").textContent === "已保存",
          );
          await shot(phase + "-after");
          await page.unroute("**/api/v1/workflows/*");
        } else if (phase === "capture-recovery") {
          await page.locator("#library-filter").fill("没有匹配");
          await shot(phase + "-filter");
          await page.locator("#clear-library-filter").click();
          await select("构建");
          await page.locator("#node-name").fill("prepare");
          await page.locator("#apply-node").click();
          await shot(phase + "-error");
          await page.locator("#node-name").fill("build");
          await page.locator("#apply-node").click();
          await shot(phase + "-after");
        } else if (phase === "capture-evidence") {
          await select("构建");
          await page
            .locator("#node-command")
            .fill("sleep 2; echo failure >&2; exit 7");
          await page.locator("#apply-node").click();
          await page.waitForFunction(
            () => !document.querySelector("#save-workflow").disabled,
          );
          await page.locator("#close-inspector").click();
          await page.locator("#save-workflow").click();
          await page.waitForFunction(() =>
            document
              .querySelector("#draft-status")
              .textContent.startsWith("已保存"),
          );
          await page.locator("#open-launchpad").click();
          await page.locator('#run-targets input[value="build"]').check();
          await page.locator("#start-run").click();
          await page.waitForFunction(
            () => document.querySelector("#run-id").textContent,
          );
          await page.locator("#toggle-console").click();
          await shot(phase + "-running");
          await page.waitForFunction(
            () =>
              document.querySelector("#run-status").textContent === "failed",
          );
          await page.locator("#fact-filter").selectOption("failed");
          await page.locator("#console-body").evaluate((body) => {
            body.scrollTop = 0;
          });
          await shot(phase + "-summary");
          await page.locator("#run-nodes .node-fact:visible summary").click();
          await shot(phase + "-after");
        }
        await page.waitForTimeout(500);
        await page
          .context()
          .tracing.stop({ path: path.join(screenshotRoot, phase + ".zip") });
      }
      console.log("PASS usability: " + phase);
    }
    assert.deepEqual(errors, []);
  } finally {
    await browser?.close();
    if (video)
      fs.renameSync(
        await video.path(),
        path.join(process.env.RUNCRADLE_USABILITY_VIDEO, phase + ".webm"),
      );
    server.kill("SIGTERM");
  }
}
main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
