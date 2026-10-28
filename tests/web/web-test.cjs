"use strict";
const fs = require("node:fs"),
  path = require("node:path"),
  os = require("node:os"),
  assert = require("node:assert/strict");
const { spawn, execFileSync } = require("node:child_process");
const cacheRoot = process.env.RUNCRADLE_CACHE_DIR || path.resolve(".cache");
const workRoot = process.env.RUNCRADLE_WORK_DIR || path.resolve(".work");
process.env.TMPDIR ||= path.resolve(".work/tmp");
fs.mkdirSync(process.env.TMPDIR, { recursive: true });
const moduleRoot =
  process.env.RUNCRADLE_PLAYWRIGHT_MODULE_DIR ||
  path.join(cacheRoot, "web-test/node_modules");
let playwright;
try {
  playwright = require(
    require.resolve("playwright", { paths: [moduleRoot, ...module.paths] }),
  );
  assert.equal(
    require(
      require.resolve("playwright/package.json", {
        paths: [moduleRoot, ...module.paths],
      }),
    ).version,
    "1.62.1",
  );
} catch (e) {
  throw new Error(
    "Playwright 1.62.1 required; explicitly run make prepare-web-test or set RUNCRADLE_PLAYWRIGHT_MODULE_DIR: " +
      e.message,
  );
}
const chrome =
  process.env.RUNCRADLE_BROWSER ||
  path.join(cacheRoot, "web-test/chrome/chrome-linux64/chrome");
assert.match(
  execFileSync(chrome, ["--version"], { encoding: "utf8" }),
  /155\.0\.8059\.39/,
);
const engine =
  process.env.RUNCRADLE_PROCESS_COMPOSE ||
  path.join(cacheRoot, "process-compose/v1.122.0/process-compose");
const binary = path.join(workRoot, "bin/runcradle"),
  reportRoot = path.join(workRoot, "w35");
fs.mkdirSync(reportRoot, { recursive: true });
const records = [];
async function host(dir) {
  const child = spawn(
    binary,
    ["serve", "--workspace", dir, "--process-compose", engine],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  let stderr = "";
  child.stderr.on("data", (b) => (stderr += b.toString().slice(-4096)));
  const ready = await new Promise((resolve, reject) => {
    let raw = "";
    const timer = setTimeout(() => {
      child.kill("SIGTERM");
      reject(new Error("serve startup exceeded 5 seconds"));
    }, 5000);
    child.stdout.on("data", (b) => {
      raw += b;
      const end = raw.indexOf("\n");
      if (end >= 0) {
        clearTimeout(timer);
        try {
          resolve(JSON.parse(raw.slice(0, end)));
        } catch (e) {
          reject(e);
        }
      }
    });
    child.on("exit", (code) => {
      clearTimeout(timer);
      reject(new Error(`serve exited ${code}: ${stderr}`));
    });
  });
  return {
    child,
    ready,
    close: async () => {
      if (child.exitCode !== null) return;
      child.kill("SIGTERM");
      await new Promise((resolve, reject) => {
        const timer = setTimeout(
          () => reject(new Error("serve shutdown exceeded 12 seconds")),
          12000,
        );
        child.once("exit", () => {
          clearTimeout(timer);
          resolve();
        });
      });
    },
  };
}
async function record(name, fn) {
  const start = Date.now();
  try {
    await fn();
    records.push({ name, passed: true, elapsed_ms: Date.now() - start });
  } catch (e) {
    records.push({
      name,
      passed: false,
      error: e.message,
      elapsed_ms: Date.now() - start,
    });
    throw e;
  }
}
function fixture(dir) {
  fs.copyFileSync(
    "examples/w2-http/process-compose.yaml",
    path.join(dir, "process-compose.yaml"),
  );
  let native = fs
    .readFileSync(path.join(dir, "process-compose.yaml"), "utf8")
    .replace("--exit-code, '0'", "--exit-code, '7'");
  fs.writeFileSync(path.join(dir, "process-compose.yaml"), native);
  let script = fs
    .readFileSync("examples/w2-http/workflow.py", "utf8")
    .replace("import argparse", "import argparse\nimport json\nimport os")
    .replace(
      'Path("checked").write_bytes(content)',
      'Path("checked").write_bytes(content)\n            Path("values.json").write_text(json.dumps([os.environ.get("TEST_SCOPE"), os.environ.get("BIG")]))',
    );
  fs.writeFileSync(path.join(dir, "workflow.py"), script);
  fs.writeFileSync(
    path.join(dir, "ui.json"),
    '{"version":1,"title":"W3 HTTP","inputs":[{"name":"scope","type":"string","default":"smoke","bindings":[{"process":"check","field":"environment","key":"TEST_SCOPE"}]},{"name":"number","type":"integer","default":9223372036854775807,"bindings":[{"process":"check","field":"environment","key":"BIG"}]}],"presets":[{"id":"quick","label":"快速","values":{"scope":"preset"}}]}',
  );
}
// Exercise the public workbench controls when a previously inline surface is now a dialog.
async function reveal(page, selector) {
  const source = [
    "#native-source",
    "#ui-source",
    "#source-format",
    "#edit-node",
    "#edit-field",
    "#field-value",
    "#apply-field",
    "#remove-field",
    "#reimport-workspace",
    "#version-select",
  ];
  const launch =
    selector.startsWith("[data-input=") ||
    [
      "#run-targets",
      "#start-run",
      "#retain-evidence",
      "#preset-select",
    ].includes(selector);
  const imports = ["#import-file-path", "#import-ui-path", "#import-workspace"];
  const target = source.includes(selector)
    ? "source-dialog"
    : launch
      ? "launchpad"
      : imports.includes(selector)
        ? "file-picker"
        : null;
  if (
    !["#apply-reimport", "#dismiss-reimport", "#reload-latest"].includes(
      selector,
    )
  ) {
    for (const d of await page.locator("dialog[open]").all()) {
      if ((await d.getAttribute("id")) !== target) await d.press("Escape");
    }
  }
  if (target && !(await page.locator("#" + target).evaluate((d) => d.open))) {
    if (
      target === "file-picker" &&
      !(await page.locator("#open-import").isVisible())
    )
      await page.locator("#toggle-sidebar").click();
    await page
      .locator(
        target === "source-dialog"
          ? "#open-source"
          : target === "launchpad"
            ? "#open-launchpad"
            : "#open-import",
      )
      .click();
  }
  if (
    target === "launchpad" &&
    [
      "#run-targets",
      "#retain-evidence",
      "#timeout-ms",
      "#preset-name",
      "#save-preset",
      "#delete-preset",
    ].includes(selector)
  )
    await page.locator(".launch-advanced").evaluate((d) => {
      d.open = true;
    });
  if (
    [
      "#edit-node",
      "#edit-field",
      "#field-value",
      "#apply-field",
      "#remove-field",
    ].includes(selector) &&
    !(await page.locator(".legacy-editor").evaluate((e) => e.open))
  )
    await page.locator(".legacy-editor summary").click();
  if (selector.startsWith("#run-history")) {
    if (!(await page.locator("#history-tab").isVisible()))
      await page.locator("#toggle-sidebar").click();
    await page.locator("#history-tab").click();
  }
  if (
    selector === "#new-workflow" &&
    !(await page.locator(selector).isVisible())
  )
    await page.locator("#toggle-sidebar").click();
}
function act(page, selector) {
  const locator = page.locator(selector);
  const actions = {};
  for (const method of ["click", "fill", "selectOption", "focus", "check"]) {
    actions[method] = async (...args) => {
      await reveal(page, selector);
      return locator[method](...args);
    };
  }
  return actions;
}
async function main() {
  const browser = await playwright.chromium.launch({
    executablePath: chrome,
    headless: true,
    env: {
      ...process.env,
      XDG_CONFIG_HOME: path.join(cacheRoot, "xdg-config"),
      XDG_CACHE_HOME: path.join(cacheRoot, "xdg-cache"),
    },
  });
  assert.equal(browser.version(), "155.0.8059.39");
  try {
    for (const viewport of [
      { width: 1440, height: 1000 },
      { width: 390, height: 844 },
    ]) {
      const dir = fs.mkdtempSync(path.join(reportRoot, "browser-"));
      fixture(dir);
      const server = await host(dir);
      const context = await browser.newContext({
        viewport,
        acceptDownloads: true,
      });
      const page = await context.newPage();
      page.setDefaultTimeout(10000);
      const errors = [],
        external = [];
      page.on("pageerror", (e) => errors.push(e.message));
      page.on("request", (r) => {
        if (
          /^https?:/.test(r.url()) &&
          !r.url().startsWith(server.ready.url + "/")
        )
          external.push(r.url());
      });
      try {
        await record(
          `${viewport.width}: import/edit/fail/fix/succeed/reuse/history`,
          async () => {
            await page.goto(server.ready.pairing_url);
            await page.locator("#library-title").waitFor({ state: "attached" });
            assert.match(await page.title(), /RunCradle/);
            await page.waitForFunction(() =>
              document
                .querySelector("#engine-state")
                .textContent.includes("process-compose"),
            );
            await act(page, "#import-file-path").fill("process-compose.yaml");
            await act(page, "#import-ui-path").fill("ui.json");
            await act(page, "#import-workspace").click();
            await page.waitForFunction(
              () => document.querySelectorAll("#step-list button").length === 3,
            );
            assert.equal(fs.existsSync(path.join(dir, "checked")), false);
            await act(page, "#save-workflow").click();
            await page.waitForFunction(
              () =>
                document.querySelector("#workflow-id").textContent.length ===
                64,
            );
            const workflowID = await page.locator("#workflow-id").textContent();
            const firstRevision = await page
              .locator("#saved-revision")
              .textContent();
            await act(page, '[data-input="number"]').fill("9007199254740993");
            await act(page, '[data-input="scope"]').fill("smoke; literal");
            await act(page, "#retain-evidence").check();
            await act(page, "#run-targets").selectOption("check");
            await act(page, "#start-run").click();
            await page.waitForFunction(
              () =>
                document.querySelector("#run-status").textContent === "failed",
            );
            const firstRun = await page.locator("#run-id").textContent();
            await page.locator("#toggle-console").click();
            assert.equal(
              await page
                .locator("#run-nodes .node-fact strong")
                .first()
                .textContent(),
              "check",
              "failed target is first in evidence overview",
            );
            await page
              .getByRole("button", { name: "定位节点 check", exact: true })
              .click();
            assert.equal(
              await page.locator("#inspector-id").textContent(),
              "check",
            );
            await page.locator("#close-inspector").click();
            assert.match(
              await page.locator("#run-nodes").innerText(),
              /check[\s\S]*7/,
            );
            assert.deepEqual(
              JSON.parse(
                fs.readFileSync(path.join(dir, "values.json"), "utf8"),
              ),
              ["smoke; literal", "9007199254740993"],
            );
            assert.match(
              await page.locator("#run-values").textContent(),
              /9007199254740993/,
              "runtime parameter text: " +
                (await page.locator("#run-values").textContent()),
            );
            await page.locator('#run-graph [aria-label="check"]').click();
            await page.locator("#node-facts .alert").waitFor();
            assert.match(
              await page.locator("#node-facts").innerText(),
              /exit_code[\s\S]*7/,
            );
            await page.screenshot({
              path: path.join(
                os.tmpdir(),
                `runcradle-w35-failure-inspector-${viewport.width}.png`,
              ),
              fullPage: false,
            });
            await page.locator("#close-inspector").click();
            await act(page, "#edit-node").selectOption("check");
            await act(page, "#edit-field").selectOption("entrypoint");
            await act(page, "#field-value").fill(
              '["python3","workflow.py","check","--exit-code","0"]',
            );
            await act(page, "#apply-field").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#draft-status")
                .textContent.includes("未保存"),
            );
            await act(page, "#save-workflow").click();
            await page.waitForFunction(
              (old) =>
                document.querySelector("#saved-revision").textContent !== old,
              firstRevision,
            );
            await act(page, "#run-targets").selectOption("check");
            await act(page, "#start-run").click();
            await page.waitForFunction(
              () =>
                document.querySelector("#run-status").textContent ===
                "succeeded",
            );
            const secondRun = await page.locator("#run-id").textContent();
            assert.notEqual(firstRun, secondRun);
            assert.ok(
              await page.evaluate(
                () => document.documentElement.scrollWidth <= innerWidth + 1,
              ),
            );
            await page.screenshot({
              path: path.join(
                os.tmpdir(),
                `runcradle-w3-${viewport.width}.png`,
              ),
              fullPage: true,
            });
            const before = fs.statSync(path.join(dir, "checked")).mtimeMs;
            await act(
              page,
              `#run-history button[data-run="${firstRun}"]`,
            ).click();
            await page.waitForFunction(
              (id) => document.querySelector("#run-id").textContent === id,
              firstRun,
            );
            assert.equal(
              await page.locator("#run-revision").textContent(),
              firstRevision,
            );
            await page.reload();
            await page.waitForFunction(
              () =>
                document.querySelector("#run-status").textContent === "failed",
            );
            assert.equal(
              fs.statSync(path.join(dir, "checked")).mtimeMs,
              before,
            );
            const second = await context.newPage();
            await second.goto(server.ready.url + "/?workflow=" + workflowID);
            await second
              .locator("#saved-revision")
              .waitFor({ state: "attached" });
            await second.waitForFunction(
              () =>
                document.querySelector("#saved-revision").textContent.length ===
                64,
            );
            const ui = await page.locator("#ui-source").inputValue();
            await act(page, "#ui-source").fill(
              ui.replace(/"title"\s*:\s*"W3 HTTP"/, '"title":"first writer"'),
            );
            await act(page, "#save-workflow").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#draft-status")
                .textContent.includes("已保存"),
            );
            await act(second, "#ui-source").fill(
              ui.replace(/"title"\s*:\s*"W3 HTTP"/, '"title":"second writer"'),
            );
            await act(second, "#save-workflow").click();
            await second.locator("#conflict").waitFor();
            assert.match(
              await second.locator("#ui-source").inputValue(),
              /second writer/,
            );
            await second.close();
          },
        );
        await record(
          `${viewport.width}: reimport previews before changing a saved draft`,
          async () => {
            const before = await page.locator("#native-source").inputValue(),
              identity = await page.locator("#workflow-id").textContent();
            const file = fs.readFileSync(
              path.join(dir, "process-compose.yaml"),
              "utf8",
            );
            await act(page, "#reimport-workspace").click();
            await page.locator("#reimport-preview").waitFor();
            assert.equal(
              await page.locator("#native-source").inputValue(),
              before,
            );
            assert.match(
              await page.locator("#reimport-old").textContent(),
              /0/,
            );
            assert.match(
              await page.locator("#reimport-new").textContent(),
              /7/,
            );
            await act(page, "#apply-reimport").click();
            assert.equal(
              await page.locator("#workflow-id").textContent(),
              identity,
            );
            assert.equal(
              await page.locator("#native-source").inputValue(),
              file,
            );
            await act(page, "#save-workflow").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#draft-status")
                .textContent.includes("已保存"),
            );
            assert.equal(
              fs.readFileSync(path.join(dir, "process-compose.yaml"), "utf8"),
              file,
            );
          },
        );
        await record(
          `${viewport.width}: connection/stop/keyboard/safe text`,
          async () => {
            await act(page, "#new-workflow").click();
            await act(page, "#native-source").fill(
              "processes: {a: {command: 'sleep 3', shutdown: {timeout_seconds: 1}, availability: {exit_on_end: true}}}",
            );
            await act(page, "#ui-source").fill("");
            await act(page, "#save-workflow").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#draft-status")
                .textContent.includes("已保存"),
            );
            await act(page, "#run-targets").selectOption("a");
            await act(page, "#start-run").click();
            await page.waitForFunction(
              () =>
                document.querySelector("#run-id").textContent.length === 32 &&
                document.querySelector("#run-status").textContent !==
                  "succeeded",
            );
            await context.setOffline(true);
            await page.waitForFunction(() =>
              document
                .querySelector("#connection-state")
                .textContent.includes("断"),
            );
            await context.setOffline(false);
            await page.waitForFunction(
              () =>
                document.querySelector("#run-status").textContent ===
                "succeeded",
              null,
              { timeout: 12000 },
            );
            await act(page, "#start-run").click();
            await page.waitForFunction(
              () =>
                document.querySelector("#run-status").textContent !==
                "succeeded",
            );
            await act(page, "#stop-run").click();
            await page.waitForFunction(
              () =>
                document.querySelector("#run-reason").textContent ===
                "cancelled",
            );
            await act(page, "#new-workflow").focus();
            await page.keyboard.press("Enter");
            await act(page, "#ui-source").fill(
              '{"version":1,"title":"<img src=x onerror=window.__pwned=1>"}',
            );
            await act(page, "#validate-draft").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#workflow-title")
                .textContent.includes("<img"),
            );
            assert.equal(await page.locator("img").count(), 0);
            assert.equal(await page.evaluate(() => window.__pwned), undefined);
            assert.deepEqual(errors, []);
            assert.deepEqual(external, []);
          },
        );
        await record(
          `${viewport.width}: selecting another run cannot stop unseen facts`,
          async () => {
            const definition = await page.evaluate(async () => {
              const list = await (await fetch("/api/v1/workflows")).json();
              return (
                await fetch(
                  `/api/v1/workflows/${list.workflows[0].workflow_id}`,
                )
              ).json();
            });
            const race = await context.newPage(),
              a = "a".repeat(32),
              b = "b".repeat(32),
              stops = [];
            let release,
              sawB = false;
            const delayed = new Promise((resolve) => (release = resolve));
            const snapshot = (id) => ({
              schema_version: 1,
              run_id: id,
              revision: definition.revision,
              workflow_id: definition.workflow.workflow_id,
              nodes: [],
              target_nodes: ["a"],
              engine_exit_code: null,
              owned: true,
              final: false,
              reason: "running",
              task_result: "pending",
              history_integrity: "partial",
              cleanup: "unknown",
              io_status: "unknown",
              values_text: "{}",
            });
            try {
              await race.route("**/api/v1/runs**", async (route) => {
                const segments = new URL(route.request().url()).pathname.split(
                    "/",
                  ),
                  id = segments[4],
                  suffix = segments[5],
                  fulfill = (data) =>
                    route.fulfill({
                      status: 200,
                      contentType: "application/json",
                      body: JSON.stringify(data),
                    });
                if (!id)
                  return fulfill({
                    runs: [snapshot(a), snapshot(b)],
                    has_more: false,
                    next_offset: 2,
                  });
                if (suffix === "events") return route.abort();
                if (suffix === "stop") {
                  stops.push(id);
                  return fulfill({ requested: true });
                }
                if (id === b) {
                  sawB = true;
                  await delayed;
                }
                return fulfill(
                  suffix === "definition" ? definition : snapshot(id),
                );
              });
              await race.goto(
                server.ready.url +
                  "/?workflow=" +
                  definition.workflow.workflow_id,
              );
              await race.waitForFunction(
                () => !document.querySelector("#new-workflow").disabled,
              );
              await act(race, `#run-history button[data-run="${a}"]`).click();
              await race.waitForFunction(
                (a) => document.querySelector("#run-id").textContent === a,
                a,
              );
              assert.equal(await race.locator("#stop-run").isEnabled(), true);
              await race.waitForFunction(
                () => !document.querySelector("#new-workflow").disabled,
              );
              await act(race, `#run-history button[data-run="${b}"]`).click();
              await race.waitForFunction(() =>
                document
                  .querySelector("#stream-state")
                  .textContent.includes("正在读取"),
              );
              assert.equal(sawB, true);
              assert.equal(
                await race.locator("#stop-run").isEnabled(),
                false,
                "stop remained active while another run was being read",
              );
              await race
                .locator("#stop-run")
                .evaluate((button) => button.click());
              assert.deepEqual(stops, []);
              release();
              await race.waitForFunction(
                (b) => document.querySelector("#run-id").textContent === b,
                b,
              );
              await act(race, "#stop-run").click();
              await race.waitForFunction(() =>
                document
                  .querySelector("#notice")
                  .textContent.includes("停止请求"),
              );
              assert.deepEqual(stops, [b]);
            } finally {
              release();
              await race.close();
            }
          },
        );
        await record(
          `${viewport.width}: inspector follows source edits without overwriting them`,
          async () => {
            await act(page, "#new-workflow").click();
            if (
              viewport.width < 700 &&
              (await page.locator("#sidebar").isVisible())
            )
              await page.locator("#toggle-sidebar").click();
            await page.locator("#editor-graph .graph-node").click();
            await act(page, "#native-source").fill(
              "processes: {check: {command: echo NEW, environment: [SOURCE=new]}}",
            );
            await page.locator("#source-apply").click();
            await page.waitForFunction(
              () => !document.querySelector("#source-dialog").open,
            );
            assert.equal(
              await page.locator("#node-command").inputValue(),
              "echo NEW",
            );
            assert.equal(await page.locator(".env-value").inputValue(), "new");
            await page.locator("#apply-node").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#native-source")
                .value.includes("echo NEW"),
            );
            await page.locator("#close-inspector").click();
          },
        );
        await record(
          `${viewport.width}: WB-01..07 zero JSON workbench and graph navigation`,
          async () => {
            await act(page, "#new-workflow").click();
            if (
              (await page.locator("#sidebar").isVisible()) &&
              viewport.width < 700
            )
              await page.locator("#toggle-sidebar").click();
            await page.locator("#editor-graph [data-node]").first().click();
            await page.locator("#node-advanced summary").click();
            await page.locator("#node-exit-end").uncheck();
            await page.locator("#add-env").click();
            await page.locator(".env-key").fill("COUNT");
            await page.locator(".env-value").fill("9007199254740993");
            await page
              .getByRole("button", { name: "绑定运行参数", exact: true })
              .click();
            await page.locator("#bind-dialog").waitFor();
            await page.locator("#bind-name").fill("count");
            await page.locator("#bind-type").selectOption("integer");
            await page.locator("#confirm-bind").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#ui-source")
                .value.includes("9007199254740993"),
            );
            assert.equal(
              await page.locator(".bound-param").textContent(),
              "◇ count",
            );
            await page.locator("#close-inspector").click();
            await page.locator("#add-node").click();
            await page.locator("#add-name").fill("after");
            await page.locator("#confirm-add").click();
            await page
              .locator("#inspector-id")
              .filter({ hasText: "after" })
              .waitFor();
            await page.locator("#add-dependency").click();
            await page.locator(".dep-row select").first().selectOption("check");
            await page.locator("#apply-node").click();
            await page.waitForFunction(
              () =>
                document.querySelectorAll("#editor-graph .edge").length === 1,
            );
            await page.locator("#node-name").fill("renamed");
            await page.locator("#node-label").fill("后续检查");
            await page.locator("#apply-node").click();
            await page.waitForFunction(
              () =>
                document.querySelector("#inspector-id").textContent ===
                "renamed",
            );
            await page.locator("#close-inspector").click();
            await page.locator("#trace-up").click();
            await page.locator("#map-toggle").click();
            assert.equal(await page.locator("#minimap svg").count(), 1);
            const before = await page
              .locator("#editor-graph svg")
              .getAttribute("width");
            await page.locator("#zoom-in").click();
            assert.notEqual(
              await page.locator("#editor-graph svg").getAttribute("width"),
              before,
            );
            await page.locator("#visual-style").selectOption("teal");
            await page.locator("#theme-toggle").click();
            await page.locator("#theme-toggle").click();
            await page.locator("#visual-style").selectOption("blue");
            await page.locator("#open-search").click();
            await page.locator("#global-search").fill("COUNT");
            await page.locator("#search-results button").first().click();
            await page.waitForFunction(
              () =>
                document.querySelector("#inspector-id").textContent === "check",
            );
            await page.locator("#close-inspector").click();
            await act(page, "#save-workflow").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#draft-status")
                .textContent.includes("已保存"),
            );
            await act(page, "#run-targets").selectOption("renamed");
            await page
              .locator('[data-input="count"]')
              .fill("9223372036854775807");
            await page.locator("#preset-name").fill("exact");
            await page.locator("#save-preset").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#ui-source")
                .value.includes("9223372036854775807"),
            );
            await act(page, "#save-workflow").click();
            await page.waitForFunction(() =>
              document
                .querySelector("#draft-status")
                .textContent.includes("已保存"),
            );
            await act(page, "#preset-select").selectOption("exact");
            assert.equal(
              await page.locator('[data-input="count"]').inputValue(),
              "9223372036854775807",
            );
            assert.equal(await page.locator("[data-override]").count(), 0);
            await act(page, "#run-targets").selectOption("renamed");
            await act(page, "#start-run").click();
            await page.waitForFunction(
              () =>
                document.querySelector("#run-status").textContent ===
                "succeeded",
            );
            assert.equal(
              await page.locator("#launchpad").evaluate((d) => d.open),
              false,
            );
            assert.equal(
              await page.locator("#monitor-mode").getAttribute("aria-pressed"),
              "true",
            );
            assert.equal(
              await page
                .locator('#run-graph .edge[data-edge-state="satisfied"]')
                .count(),
              1,
            );
            await page.locator("#design-mode").click();
            await page.locator("#fit").click();
            await page.screenshot({
              path: path.join(
                os.tmpdir(),
                `runcradle-w35-workbench-${viewport.width}.png`,
              ),
              fullPage: false,
            });
            await page.locator('#editor-graph [aria-label="后续检查"]').click();
            await page.locator("#delete-node").click();
            await page.waitForFunction(
              () =>
                document.querySelectorAll("#editor-graph .graph-node")
                  .length === 1,
            );
            await page.locator("#editor-graph .graph-node").click();
            await page
              .getByRole("button", { name: "解除参数绑定 count", exact: true })
              .click();
            await page.waitForFunction(
              () =>
                !document
                  .querySelector("#ui-source")
                  .value.includes('"name": "count"'),
            );
            const meta = JSON.parse(
              await page.locator("#ui-source").inputValue(),
            );
            assert.deepEqual(meta.presets[0].values, {});
            assert.deepEqual(errors, []);
          },
        );
        await record(
          `${viewport.width}: static export stays offline`,
          async () => {
            const out = path.join(dir, "offline.html");
            execFileSync(binary, [
              "workflow",
              "render",
              "--workspace",
              dir,
              "--file",
              "process-compose.yaml",
              "--ui",
              "ui.json",
              "--out",
              "offline.html",
            ]);
            const offline = await context.newPage(),
              requests = [];
            offline.on("request", (r) => {
              if (/^https?:/.test(r.url())) requests.push(r.url());
            });
            await offline.goto("file://" + out);
            assert.equal(
              await offline.locator("input,textarea,form").count(),
              0,
            );
            await offline.locator(".step").last().click();
            assert.match(
              await offline.locator("#node-detail").innerText(),
              /check/,
            );
            assert.deepEqual(requests, []);
            await offline.keyboard.press("/");
            await offline.locator("#offline-search").fill("check");
            assert.equal(await offline.locator(".step:visible").count(), 1);
            await offline.locator("#offline-search").fill("");
            await offline.locator("#offline-up").click();
            await offline.locator("#offline-map-toggle").click();
            assert.equal(await offline.locator("#offline-map svg").count(), 1);
            await offline.locator("#zoom-in").click();
            await offline.locator("#offline-theme").click();
            await offline.locator("#offline-palette").selectOption("teal");
            await offline.screenshot({
              path: path.join(
                os.tmpdir(),
                `runcradle-w35-offline-${viewport.width}.png`,
              ),
              fullPage: false,
            });
            const conditions = [
              "process_started",
              "process_completed",
              "process_completed_successfully",
              "process_healthy",
              "process_log_ready",
            ];
            const config = {
              processes: {
                root: {
                  command: "echo ready",
                  readiness_probe: { exec: { command: "true" } },
                },
              },
            };
            conditions.forEach(
              (condition, i) =>
                (config.processes["next" + i] = {
                  command: "echo next",
                  depends_on: { root: { condition } },
                }),
            );
            fs.writeFileSync(
              path.join(dir, "five.json"),
              JSON.stringify(config),
            );
            execFileSync(binary, [
              "workflow",
              "render",
              "--workspace",
              dir,
              "--file",
              "five.json",
              "--out",
              "five.html",
            ]);
            await offline.goto("file://" + path.join(dir, "five.html"));
            assert.equal(await offline.locator(".edge").count(), 5);
            for (const label of [
              "启动后",
              "结束后",
              "成功完成后",
              "健康就绪后",
              "日志就绪后",
            ])
              assert.ok(
                (
                  await offline.locator(".edge text").allTextContents()
                ).includes(label),
              );
            assert.deepEqual(requests, []);
            await offline.close();
          },
        );
      } catch (e) {
        fs.writeFileSync(
          path.join(reportRoot, "browser-failure.txt"),
          (await page.locator("body").innerText()) +
            "\nSOURCE\n" +
            (await page.locator("#native-source").inputValue()),
        );
        await page.screenshot({
          path: path.join(os.tmpdir(), "runcradle-w3-failure.png"),
          fullPage: true,
        });
        throw e;
      } finally {
        await context.setOffline(false);
        await context.close();
        await server.close();
      }
    }
  } finally {
    await browser.close();
    fs.writeFileSync(
      path.join(reportRoot, "web-results.json"),
      JSON.stringify(
        {
          browser: "155.0.8059.39",
          playwright: "1.62.1",
          browserPlugin: "absent",
          reason:
            "Browser plugin not available; approved pinned Playwright workflow",
          records,
        },
        null,
        2,
      ) + "\n",
    );
  }
  console.log(
    JSON.stringify({
      cases: records.length,
      passed: records.filter((r) => r.passed).length,
      browser: "155.0.8059.39",
      playwright: "1.62.1",
    }),
  );
}
main().catch((e) => {
  console.error(e.stack);
  process.exitCode = 1;
});
