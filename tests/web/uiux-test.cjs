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
  process.env.RUNCRADLE_SCREENSHOTS || path.join(root, "screenshots");
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
    const failures = [];
    async function check(name, fn) {
      try {
        await fn();
        console.log("PASS uiux: " + name);
      } catch (e) {
        console.log(
          "DIAGNOSTIC",
          await page.locator("#notice").textContent(),
          await page.locator("#run-status").textContent(),
          await page.locator("#run-reason").textContent(),
        );
        failures.push(name + ": " + e.message);
      }
    }
    await check("pending inputs visible in saved status", async () => {
      await select("构建");
      await page.locator("#node-command").fill("echo pending-uiux");
      assert.match(
        await page.locator("#draft-status").textContent(),
        /1.*未应用/,
      );
    });
    await page.locator("#discard-node").click();
    await page.locator("#close-inspector").click();
    await check("first launch requires explicit targets", async () => {
      await page.locator("#open-launchpad").click();
      const chosen = await page
        .locator("#run-targets")
        .evaluate((el) =>
          el.tagName === "SELECT"
            ? [...el.selectedOptions].map((o) => o.value)
            : [...el.querySelectorAll("input:checked")].map((o) => o.value),
        );
      assert.deepEqual(chosen, []);
      assert.equal(await page.locator("#start-run").isDisabled(), true);
    });
    await page.locator('[data-close="launchpad"]').click();
    await check("mobile canvas opens at readable 100 percent", async () => {
      await page.setViewportSize({ width: 390, height: 844 });
      await page.waitForTimeout(150);
      assert.equal(
        Number(await page.locator("#editor-graph").getAttribute("data-zoom")),
        1,
      );
    });
    await page.setViewportSize({ width: 1585, height: 992 });
    const chosen = () =>
      page
        .locator("#run-targets input:checked")
        .evaluateAll((es) => es.map((e) => e.value));
    const target = (name) =>
      page.locator(`#run-targets input[value="${name}"]`);
    async function launch() {
      await page.locator("#open-launchpad").click();
    }
    async function dismiss() {
      await page.locator('[data-close="launchpad"]').click();
    }
    async function validate() {
      await page.locator("#more-actions summary").click();
      await page.locator("#validate-draft").click();
      await page.waitForFunction(
        () => !document.getElementById("save-workflow").disabled,
      );
    }
    await check(
      "multiple pending nodes, close/reopen, failure, save and launch feedback",
      async () => {
        await select("构建");
        await page.locator("#node-command").fill("echo unapplied build");
        await select("单元测试");
        await page.locator("#node-command").fill("echo unapplied test");
        assert.match(
          await page.locator("#draft-status").textContent(),
          /2.*未应用/,
        );
        assert.equal(await page.locator(".node-unapplied").count(), 2);
        await page.locator("#close-inspector").click();
        await select("构建");
        assert.equal(
          await page.locator("#node-command").inputValue(),
          "echo unapplied build",
        );
        await page.locator("#node-name").fill("test");
        await page.locator("#apply-node").click();
        await page.locator("#inspector-error").waitFor();
        assert.equal(
          await page.locator("#node-name").getAttribute("aria-invalid"),
          "true",
          "duplicate names are rejected locally before submission",
        );
        assert.equal(
          await page.locator("#node-command").inputValue(),
          "echo unapplied build",
        );
        await page.locator("#node-name").fill("build");
        await page.locator("#save-workflow").click();
        await page.waitForFunction(() =>
          document.getElementById("notice").textContent.includes("尚未包含"),
        );
        assert.match(
          await page.locator("#native-source").inputValue(),
          /echo build/,
        );
        assert.doesNotMatch(
          await page.locator("#native-source").inputValue(),
          /unapplied/,
        );
        await launch();
        assert.equal(await page.locator("#launch-pending").isVisible(), true);
        await page.locator("#return-to-edit").click();
        assert.equal(await page.locator("#inspector").isVisible(), true);
        await select("构建");
        await page.locator("#apply-node").click();
        await page.waitForFunction(() =>
          document
            .getElementById("draft-status")
            .textContent.includes("1 个节点未应用"),
        );
        await select("单元测试");
        assert.equal(
          await page.locator("#node-command").inputValue(),
          "echo unapplied test",
        );
        await page.locator("#discard-node").click();
        assert.equal(await page.locator(".node-unapplied").count(), 0);
        await page.locator("#close-inspector").click();
      },
    );
    await check(
      "search and multiselect survive validation, edits and saving",
      async () => {
        await launch();
        await target("build").check();
        await page.locator("#target-search").fill("结果");
        assert.equal(await page.locator("#run-targets input").count(), 1);
        await target("check").check();
        assert.match(
          await page.locator("#target-summary").textContent(),
          /2.*build、check/,
        );
        await page.locator("#target-search").fill("");
        await dismiss();
        await validate();
        await launch();
        assert.deepEqual(await chosen(), ["build", "check"]);
        await dismiss();
        await page.locator("#save-workflow").click();
        await page.waitForFunction(() =>
          document
            .getElementById("draft-status")
            .textContent.startsWith("已保存"),
        );
        await launch();
        assert.deepEqual(await chosen(), ["build", "check"]);
        await dismiss();
      },
    );
    await check(
      "renamed and deleted targets removed with explanation",
      async () => {
        await select("构建");
        await page.locator("#node-name").fill("build_new");
        await page.locator("#apply-node").click();
        await page.waitForFunction(
          () =>
            document.getElementById("inspector-id").textContent === "build_new",
        );
        await launch();
        assert.deepEqual(await chosen(), ["check"]);
        assert.match(
          await page.locator("#target-notice").textContent(),
          /build/,
        );
        await dismiss();
        await page.locator("#close-inspector").click();
        await select("结果检查");
        await page.locator("#node-advanced summary").click();
        await page.locator("#delete-node").click();
        await page.waitForFunction(
          () =>
            ![...document.querySelectorAll("#run-targets input")].some(
              (e) => e.value === "check",
            ),
        );
        await launch();
        assert.deepEqual(await chosen(), []);
        assert.equal(await page.locator("#start-run").isDisabled(), true);
        await dismiss();
      },
    );
    await check(
      "node launch chooses node; history does not change next-run targets",
      async () => {
        await page.locator("#save-workflow").click();
        await page.waitForFunction(() =>
          document
            .getElementById("draft-status")
            .textContent.startsWith("已保存"),
        );
        const nodeRun = page.locator(
          '#editor-graph [aria-label="运行到 build_new"]',
        );
        await nodeRun.focus();
        await nodeRun.press("Enter");
        assert.deepEqual(await chosen(), ["build_new"]);
        await page.locator("#start-run").click();
        await page.waitForFunction(
          () =>
            ["succeeded", "success"].includes(
              document.getElementById("run-status").textContent,
            ),
          null,
          { timeout: 20000 },
        );
        await launch();
        await target("lint").check();
        await dismiss();
        if (!(await page.locator("#history-tab").isVisible()))
          await page.locator("#toggle-sidebar").click();
        await page.locator("#history-tab").click();
        await page.locator("#run-history button[data-run]").first().click();
        await launch();
        assert.deepEqual(await chosen(), ["build_new", "lint"]);
        await dismiss();
        await page.locator("#design-mode").click();
      },
    );
    await page.locator("#design-mode").click();
    await check(
      "viewport persists through panels and redraw; explicit fit reports percentage",
      async () => {
        await page.setViewportSize({ width: 1024, height: 768 });
        await page.locator("#zoom-in").click();
        const canvas = page.locator("#editor-graph");
        const zoom = await canvas.getAttribute("data-zoom");
        await canvas.evaluate((el) => el.scrollTo(180, 0));
        await page.waitForFunction(
          () => document.getElementById("editor-graph")._viewport.left === 180,
        );
        await validate();
        assert.equal(await canvas.getAttribute("data-zoom"), zoom);
        assert.equal(await canvas.evaluate((el) => el.scrollLeft), 180);
        await page.locator("#toggle-sidebar").click();
        assert.equal(await canvas.getAttribute("data-zoom"), zoom);
        await page.locator("#monitor-mode").click();
        await page.locator("#design-mode").click();
        assert.equal(await canvas.evaluate((el) => el.scrollLeft), 180);
        await select("构建");
        await page.locator("#node-command").fill("echo viewport apply");
        await canvas.evaluate((el) => el.scrollTo(180, 0));
        await page.waitForFunction(
          () => document.getElementById("editor-graph")._viewport.left === 180,
        );
        await page.locator("#apply-node").click();
        await page.waitForFunction(
          () => !document.getElementById("save-workflow").disabled,
        );
        assert.equal(
          await canvas.evaluate((el) => el.scrollLeft),
          180,
          "apply retains view",
        );
        await page.locator("#close-inspector").click();
        await page.locator("#fit").click();
        assert.ok(Number(await canvas.getAttribute("data-zoom")) < 1);
        assert.match(await page.locator("#zoom-level").textContent(), /%/);
      },
    );
    await check(
      "explicit version navigation resets targets and viewport",
      async () => {
        await page.locator("#more-actions summary").click();
        await page.locator("#open-source").click();
        const versions = await page
          .locator("#version-select option")
          .evaluateAll((es) => es.map((e) => e.value));
        await page.locator("#version-select").selectOption(versions.at(-1));
        await page.waitForFunction(
          () => !document.getElementById("save-workflow").disabled,
        );
        await page.locator('[data-close="source-dialog"]').click();
        await page.waitForFunction(
          () =>
            document.activeElement ===
            document.querySelector("#more-actions summary"),
        );
        assert.equal(
          await page
            .locator("#more-actions summary")
            .evaluate((el) => el === document.activeElement),
          true,
          "source dialog returns focus to More",
        );
        await launch();
        assert.deepEqual(await chosen(), []);
        await dismiss();
        assert.equal(
          Number(await page.locator("#editor-graph").getAttribute("data-zoom")),
          1,
        );
      },
    );
    await check(
      "same-run refresh preserves monitor zoom and scroll",
      async () => {
        await page.setViewportSize({ width: 1024, height: 768 });
        await select("构建");
        await page.locator("#node-command").fill("sleep 2; echo viewport");
        await page.locator("#apply-node").click();
        await page.waitForFunction(
          () => !document.getElementById("save-workflow").disabled,
        );
        await page.locator("#save-workflow").click();
        await page.waitForFunction(() =>
          document
            .getElementById("draft-status")
            .textContent.startsWith("已保存"),
        );
        await page.locator("#close-inspector").click();
        await launch();
        await target("build").check();
        await page.locator("#start-run").click();
        await page.locator("#run-graph svg").waitFor();
        assert.equal(
          Number(await page.locator("#run-graph").getAttribute("data-zoom")),
          1,
        );
        await page.locator("#zoom-in").click();
        const canvas = page.locator("#run-graph");
        await canvas.evaluate((el) => el.scrollTo(180, 0));
        await page.waitForFunction(
          () => document.getElementById("run-graph")._viewport.left === 180,
        );
        await page.waitForFunction(
          () =>
            document.getElementById("run-status").textContent === "succeeded",
          null,
          { timeout: 20000 },
        );
        assert.equal(Number(await canvas.getAttribute("data-zoom")), 1.2);
        assert.equal(await canvas.evaluate((el) => el.scrollLeft), 180);
        await page.locator("#design-mode").click();
        assert.equal(
          Number(await page.locator("#editor-graph").getAttribute("data-zoom")),
          1,
        );
      },
    );
    await check("reopening current history run keeps pan", async () => {
      await page.locator("#monitor-mode").click();
      if (!(await page.locator("#history-tab").isVisible()))
        await page.locator("#toggle-sidebar").click();
      await page.locator("#history-tab").click();
      const canvas = page.locator("#run-graph");
      await canvas.evaluate((el) => el.scrollTo(180, 0));
      await page.waitForFunction(
        () => document.getElementById("run-graph")._viewport.left === 180,
      );
      await page.locator("#run-history button[data-run]").first().click();
      await page.waitForFunction(
        () => document.getElementById("run-status").textContent === "succeeded",
      );
      assert.equal(
        await canvas.evaluate((el) => el.scrollLeft),
        180,
        "same run history reload preserves pan",
      );
      assert.equal(Number(await canvas.getAttribute("data-zoom")), 1.2);
    });
    await check(
      "loading another run clears previous target summary",
      async () => {
        const other = page.locator("#run-history button[data-run]").nth(1);
        const id = await other.getAttribute("data-run");
        let release;
        const hold = new Promise((resolve) => {
          release = resolve;
        });
        await page.route("**/api/v1/runs/" + id, async (route) => {
          const response = await route.fetch();
          await hold;
          await route.fulfill({ response });
        });
        try {
          await other.click();
          await page.waitForFunction(
            () =>
              document.getElementById("run-status").textContent === "读取中",
          );
          assert.equal(
            await page
              .locator("#run-overview-summary")
              .evaluate((el) => el.hidden),
            true,
          );
          assert.equal(
            await page.locator("#run-status").getAttribute("data-result"),
            "",
          );
        } finally {
          release();
          await page.unrouteAll({ behavior: "wait" });
        }
        await page.waitForFunction(
          () =>
            document.getElementById("run-status").textContent === "succeeded",
        );
        assert.equal(
          Number(await page.locator("#run-graph").getAttribute("data-zoom")),
          1,
        );
      },
    );
    await page.locator("#design-mode").click();
    await check(
      "three independent themes by four viewports: readable default, actions and screenshots",
      async () => {
        if (
          await page
            .getByRole("button", { name: "关闭消息", exact: true })
            .isVisible()
        )
          await page
            .getByRole("button", { name: "关闭消息", exact: true })
            .click();
        for (const viewport of [
          { width: 1585, height: 992 },
          { width: 1366, height: 768 },
          { width: 1024, height: 768 },
          { width: 390, height: 844 },
        ]) {
          await page.setViewportSize(viewport);
          for (const theme of ["blue-light", "teal-light", "graphite-amber"]) {
            await page.locator("#visual-style").selectOption(theme);
            assert.equal(
              await page.evaluate(
                () => document.documentElement.scrollWidth <= innerWidth,
              ),
              true,
            );
            for (const id of [
              "save-workflow",
              "open-launchpad",
              "draft-status",
              "zoom-level",
            ])
              assert.equal(
                await page.locator("#" + id).isVisible(),
                true,
                id + " remains visible",
              );
            const size = await page
              .locator("#editor-graph .node-title")
              .first()
              .evaluate(
                (el) =>
                  parseFloat(getComputedStyle(el).fontSize) *
                  Number(document.getElementById("editor-graph").dataset.zoom),
              );
            assert.equal(
              size,
              15,
              "node title renders at 15px at default scale",
            );
            await page.screenshot({
              path: path.join(
                screenshotRoot,
                `uiux-${viewport.width}-${theme}.png`,
              ),
            });
          }
        }
        await page.setViewportSize({ width: 1585, height: 992 });
        await page.locator("#visual-style").selectOption("graphite-amber");
        await select("构建");
        assert.equal(
          await page
            .locator("#inspector")
            .evaluate((el) => el.getBoundingClientRect().width),
          360,
          "desktop inspector follows approved width",
        );
        await page.locator("#node-command").fill("echo pending preview");
        await page.screenshot({
          path: path.join(screenshotRoot, "uiux-pending.png"),
        });
        await launch();
        await page.screenshot({
          path: path.join(screenshotRoot, "uiux-launch.png"),
        });
        await dismiss();
        await page.locator("#monitor-mode").click();
        await page.locator("#toggle-console").click();
        await page.screenshot({
          path: path.join(screenshotRoot, "uiux-evidence.png"),
        });
      },
    );
    assert.deepEqual(failures, [], failures.join("\n"));
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
