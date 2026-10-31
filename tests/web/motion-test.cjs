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
  ["build", "echo build", ["prepare"]],
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
      recordVideo: process.env.RUNCRADLE_MOTION_VIDEO
        ? { dir: process.env.RUNCRADLE_MOTION_VIDEO }
        : undefined,
      viewport: { width: 1585, height: 992 },
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

    const run = async (name, fn) => {
      if (phase !== "all" && phase !== name) return;
      await fn();
      console.log("PASS motion: " + name);
    };
    await run("controls", async () => {
      await page.locator("#open-launchpad").click();
      const box = page.locator("#run-targets input").first();
      await box.check();
      assert.equal(await box.isChecked(), true);
      assert.equal(
        await box.evaluate(
          (e) => !!e.nextElementSibling?.querySelector("svg path"),
        ),
        true,
      );
      await page.locator("#target-search").fill("no-match");
      await page.locator("#target-search").fill("");
      assert.equal(
        await page.locator("#run-targets input").first().isChecked(),
        true,
      );
      assert.equal(
        await page
          .locator("#run-targets input")
          .first()
          .evaluate(
            (e) => e.nextElementSibling.getAnimations({ subtree: true }).length,
          ),
        0,
      );
      await page.keyboard.press("Escape");
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.locator("#open-launchpad").click();
      await box.uncheck();
      await box.check();
      assert.equal(
        await page.evaluate(
          () =>
            document.getAnimations().filter((a) => a.playState === "running")
              .length,
        ),
        0,
      );
      await page.keyboard.press("Escape");
      await page.emulateMedia({ reducedMotion: "no-preference" });
    });
    await run("panels", async () => {
      await page.locator("#toggle-console").click();
      await page.locator("#evidence-overview-tab").focus();
      await page.evaluate(() => {
        document.addEventListener(
          "click",
          () => {
            window.motionIndicatorObserved = document
              .querySelector(".evidence-tabs .tab-indicator")
              .getAnimations()
              .some((a) => a.playState === "running");
          },
          { once: true },
        );
      });
      await page.keyboard.press("ArrowRight");
      assert.equal(
        await page.locator("#evidence-logs-tab").getAttribute("aria-selected"),
        "true",
      );
      assert.equal(
        await page
          .locator("#evidence-logs-tab")
          .evaluate((e) => e === document.activeElement),
        true,
      );
      assert.equal(
        await page.evaluate(() => window.motionIndicatorObserved),
        true,
        "explicit tab selection animates",
      );
      await page.keyboard.press("End");
      assert.equal(
        await page
          .locator("#evidence-details-tab")
          .getAttribute("aria-selected"),
        "true",
      );
      await page.keyboard.press("Home");
      assert.equal(
        await page.locator(".evidence-tabs .tab-indicator").count(),
        1,
      );
      await page.locator("#open-launchpad").click();
      await page.keyboard.press("Escape");
      assert.equal(
        await page.locator("#launchpad").evaluate((e) => e.open),
        false,
      );
      assert.equal(
        await page
          .locator("#open-launchpad")
          .evaluate((e) => e === document.activeElement),
        true,
      );
      await page.locator("#open-launchpad").click();
      assert.equal(
        await page.locator("#launchpad").evaluate((e) => e.open),
        true,
      );
      await page.keyboard.press("Escape");
    });
    await run("advanced-checkbox", async () => {
      for (const viewport of [
        { width: 1585, height: 992 },
        { width: 390, height: 844 },
      ]) {
        await page.setViewportSize(viewport);
        await select("构建");
        if (!(await page.locator("#node-advanced").getAttribute("open"))) {
          if (!(await page.locator("#node-advanced").evaluate((e) => e.open)))
            await page.locator("#node-advanced summary").click();
        }
        await page.locator("#node-exit-end").check();
        await page.locator("#node-exit-end").uncheck();
        assert.equal(await page.locator("#node-exit-end").isChecked(), false);
        await page.locator("#close-inspector").click();
      }
      await page.setViewportSize({ width: 1585, height: 992 });
    });
    await run("closed-details", async () => {
      await select("构建");
      await page.locator("#node-advanced summary").click();
      if (!(await page.locator("#node-advanced").evaluate((e) => e.open)))
        await page.locator("#node-advanced summary").click();
      await page.locator("#node-exit-end").focus();
      await page.locator("#node-advanced summary").click();
      assert.equal(
        await page.locator("#node-exit-end").evaluate((e) => {
          e.focus();
          return e === document.activeElement;
        }),
        false,
        "closed details cannot receive focus during exit animation",
      );
      await page.locator("#close-inspector").click();
    });
    await run("theme-contrast", async () => {
      const stable = await page.evaluate(() => {
        const body = document.body;
        const select = document.querySelector("#visual-style");
        select.value = "blue-light";
        select.dispatchEvent(new Event("change"));
        const card = document.querySelector("#editor-graph .graph-node > rect");
        getComputedStyle(card).fill;
        select.value = "graphite-amber";
        select.dispatchEvent(new Event("change"));
        const target = document.createElementNS(
          "http://www.w3.org/2000/svg",
          "rect",
        );
        target.style.fill = "var(--node-surface,var(--surface))";
        card.parentNode.append(target);
        const matches =
          getComputedStyle(card).fill === getComputedStyle(target).fill;
        target.remove();
        return matches;
      });
      assert.equal(stable, true, "theme surface and text change atomically");
      await page.locator("#visual-style").selectOption("blue-light");
    });
    await run("async", async () => {
      const result = await page.evaluate(async () => {
        const { beginLoading } = await import("/workbench/view.js");
        const host = document.createElement("div");
        document.body.append(host);
        const a = beginLoading(host);
        a.finish();
        await new Promise((r) => setTimeout(r, 180));
        const fast = host.querySelector(".loading-placeholder") === null;
        const b = beginLoading(host);
        const c = beginLoading(host);
        b.finish();
        await new Promise((r) => setTimeout(r, 180));
        const pending =
          host.getAttribute("aria-busy") === "true" &&
          !!host.querySelector(".loading-placeholder");
        const stale = b.current() === false;
        c.finish();
        const clear =
          !host.hasAttribute("aria-busy") &&
          !host.querySelector(".loading-placeholder");
        host.append(document.createElement("button"));
        const d = beginLoading(host);
        await new Promise((r) => setTimeout(r, 180));
        const retained =
          !!host.querySelector("button") &&
          !host.querySelector(".loading-placeholder");
        d.finish();
        host.remove();
        return { fast, pending, stale, clear, retained };
      });
      assert.deepEqual(result, {
        fast: true,
        pending: true,
        stale: true,
        clear: true,
        retained: true,
      });
    });
    await run("loading-lifecycle", async () => {
      await page.evaluate(async () => {
        const { beginLoading } = await import("/workbench/view.js");
        const host = document.createElement("div");
        host.id = "motion-test-loading";
        document.body.append(host);
        window.motionTestRequest = beginLoading(host);
      });
      await page.locator("#motion-test-loading .loading-placeholder").waitFor();
      await page.emulateMedia({ reducedMotion: "reduce" });
      assert.equal(
        await page.locator("#motion-test-loading .loading-placeholder").count(),
        1,
        "reduced motion keeps loading explanation",
      );
      await page.evaluate(() => {
        window.motionTestRequest.finish();
        document.querySelector("#motion-test-loading").remove();
      });
      await page.emulateMedia({ reducedMotion: "no-preference" });
    });
    await run("list-refresh", async () => {
      const result = await page.evaluate(async () => {
        const { reconcileChildren } = await import("/workbench/view.js");
        const host = document.createElement("div");
        document.body.append(host);
        const a = document.createElement("button"),
          b = document.createElement("button");
        host.append(a, b);
        b.focus();
        reconcileChildren(host, [a, b]);
        const stable = document.activeElement === b;
        reconcileChildren(host, [b, a]);
        const reordered = host.firstChild === b && document.activeElement === b;
        host.remove();
        return { stable, reordered };
      });
      assert.deepEqual(result, { stable: true, reordered: true });
    });
    await run("graph", async () => {
      const result = await page.evaluate(() => {
        const c = document.querySelector("#editor-graph");
        const display = c._display;
        const name = display.graph.nodes[0].Name;
        const draw = (facts, animateFacts = false) =>
          RunCradleGraph.draw(c, display, () => {}, facts, { animateFacts });
        draw([{ name, state: "running", exit_code: null }]);
        const node = c.querySelector(".graph-node");
        node.focus();
        const svg = c.firstElementChild;
        const dot = node.querySelector(".state-dot");
        const pulse = dot.getAnimations()[0];
        draw([{ name, state: "running", exit_code: null }]);
        const retained =
          node === c.querySelector(".graph-node") &&
          svg === c.firstElementChild &&
          document.activeElement === node;
        const uninterrupted = !pulse || pulse === dot.getAnimations()[0];
        draw([{ name, state: "completed", exit_code: "0" }], true);
        const pill = node.querySelector(".state-pill");
        const emphasis = pill
          .getAnimations()
          .find((a) => a.effect?.getTiming().duration === 220);
        draw([{ name, state: "completed", exit_code: "0" }], true);
        const once = !!emphasis && pill.getAnimations().includes(emphasis);
        const updated =
          node.querySelector(".node-state").textContent === "已结束" &&
          node.querySelector(".node-exit").textContent === "退出 0";
        pill.getAnimations().forEach((a) => a.cancel());
        draw([{ name, state: "failed", exit_code: "1" }], false);
        const historical = !pill
          .getAnimations()
          .some((a) => a.effect?.getTiming().duration === 220);
        return { retained, uninterrupted, updated, once, historical };
      });
      assert.deepEqual(result, {
        retained: true,
        uninterrupted: true,
        updated: true,
        once: true,
        historical: true,
      });
    });
    if (phase.startsWith("capture-")) {
      const folder = process.env.RUNCRADLE_MOTION_VIDEO;
      assert.ok(folder, "capture requires an evidence directory");
      await page
        .context()
        .tracing.start({ screenshots: true, snapshots: true });
      await page.screenshot({ path: path.join(folder, phase + "-before.png") });
      if (phase === "capture-launch") {
        await page.locator("#open-launchpad").click();
        await page.locator('#run-targets input[value="build"]').check();
        await page.locator('#run-targets input[value="lint"]').check();
        await page.locator("#launchpad summary").click();
        await page.waitForTimeout(350);
        await page.screenshot({
          path: path.join(folder, phase + "-after.png"),
        });
        await page.keyboard.press("Escape");
      }
      if (phase === "capture-edit" || phase === "capture-run") {
        await select("构建");
        await page
          .locator("#node-command")
          .fill(
            phase === "capture-run"
              ? "sleep 2; echo motion-run"
              : "echo motion-edit",
          );
        await page.waitForTimeout(300);
        await page.screenshot({
          path: path.join(folder, phase + "-pending.png"),
        });
        await page.locator("#apply-node").click();
        await page.locator("#save-workflow").click();
        await page.waitForFunction(() =>
          document
            .querySelector("#draft-status")
            .textContent.includes("已保存"),
        );
        await page.screenshot({
          path: path.join(folder, phase + "-after.png"),
        });
      }
      if (phase === "capture-run") {
        await page.locator("#open-launchpad").click();
        await page.locator('#run-targets input[value="build"]').check();
        await page.locator("#start-run").click();
        await page.waitForFunction(() =>
          document.querySelector("#run-graph .running"),
        );
        await page.screenshot({
          path: path.join(folder, phase + "-running.png"),
        });
        const cdp = await page.context().newCDPSession(page);
        await cdp.send("Performance.enable");
        const beforeMetrics = Object.fromEntries(
          (await cdp.send("Performance.getMetrics")).metrics.map((m) => [
            m.name,
            m.value,
          ]),
        );
        const perf = await page.evaluate(() => {
          const container = document.querySelector("#run-graph");
          const nodes = [...container.querySelectorAll(".graph-node")];
          const display = container._display;
          // Replaying the same observed state measures redundant-refresh work, not engine behavior.
          const facts = display.graph.nodes.map((p, i) => ({
            name: p.Name,
            state: nodes[i].dataset.factState,
            health: nodes[i].dataset.factHealth,
            exit_code: null,
          }));
          RunCradleGraph.draw(container, display, () => {}, facts);
          const observer = new MutationObserver(() => {});
          observer.observe(container, { childList: true, subtree: true });
          const started = performance.now();
          for (let i = 0; i < 50; i++)
            RunCradleGraph.draw(container, display, () => {}, facts);
          const duration = performance.now() - started;
          const childMutations = observer.takeRecords().length;
          observer.disconnect();
          return {
            refreshes: 50,
            durationMs: duration,
            childMutations,
            retainedNodes: nodes.every(
              (n, i) => n === container.querySelectorAll(".graph-node")[i],
            ),
          };
        });
        const afterMetrics = Object.fromEntries(
          (await cdp.send("Performance.getMetrics")).metrics.map((m) => [
            m.name,
            m.value,
          ]),
        );
        perf.browserMetricsDelta = Object.fromEntries(
          ["LayoutCount", "RecalcStyleCount", "TaskDuration", "Nodes"].map(
            (name) => [name, afterMetrics[name] - beforeMetrics[name]],
          ),
        );
        await cdp.detach();
        fs.writeFileSync(
          path.join(folder, "refresh-performance.json"),
          JSON.stringify(perf, null, 2),
        );
        assert.equal(perf.childMutations, 0);
        assert.equal(perf.retainedNodes, true);
        await page.waitForFunction(
          () =>
            document.querySelector("#run-status").textContent === "succeeded",
        );
        await page.locator("#toggle-console").click();
        await page.waitForTimeout(300);
        await page.screenshot({
          path: path.join(folder, phase + "-completed.png"),
        });
      }
      await page.waitForTimeout(400);
      await page
        .context()
        .tracing.stop({ path: path.join(folder, phase + ".zip") });
      console.log("PASS motion recording: " + phase);
    }
    assert.deepEqual(errors, []);
  } finally {
    await browser?.close();
    if (video && process.env.RUNCRADLE_MOTION_VIDEO) {
      const recorded = await video.path();
      fs.renameSync(
        recorded,
        path.join(process.env.RUNCRADLE_MOTION_VIDEO, phase + ".webm"),
      );
    }
    server.kill("SIGTERM");
  }
}
main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
