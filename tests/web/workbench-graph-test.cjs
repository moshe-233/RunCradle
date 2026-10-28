"use strict";
const assert = require("node:assert/strict"),
  fs = require("node:fs"),
  vm = require("node:vm");
const scope = { window: {} };
vm.runInNewContext(
  fs.readFileSync("internal/webui/assets/graph.js", "utf8"),
  scope,
);
const edge = scope.window.RunCradleGraph.edgeState;
const nodes = [{ name: "a", success_exit_codes: ["0", "7"] }];
const run = {
  name: "a",
  state: "running",
  started_at: "2026-10-25T00:00:00Z",
  health: "not_ready",
  exit_code: null,
};
const done = {
  ...run,
  state: "completed",
  ended_at: "2026-10-25T00:00:01Z",
  exit_code: 7,
};
function state(condition, facts) {
  return edge(
    { FromName: "a", ToName: "b", Condition: condition },
    nodes,
    facts,
  );
}
assert.equal(state("process_started", [run]).state, "satisfied");
assert.equal(state("process_completed", [run]).state, "waiting");
assert.equal(
  state("process_completed", [{ ...done, exit_code: 8 }]).state,
  "satisfied",
);
assert.equal(
  state("process_completed_successfully", [done]).state,
  "satisfied",
);
assert.equal(
  state("process_completed_successfully", [{ ...done, exit_code: 8 }]).state,
  "blocked",
);
assert.equal(state("process_healthy", [run]).state, "waiting");
assert.equal(
  state("process_healthy", [{ ...run, health: "ready" }]).state,
  "satisfied",
);
assert.equal(state("process_log_ready", [run]).state, "idle");
assert.match(state("process_log_ready", [run]).reason, /证据不足/);
assert.equal(
  state("process_log_ready", [run, { name: "b", state: "running" }]).state,
  "satisfied",
);
assert.match(
  state("process_log_ready", [run, { name: "b", state: "running" }]).reason,
  /推断/,
);
assert.equal(
  state("process_completed_successfully", [{ ...done, exit_code: null }]).state,
  "idle",
);
const diff = scope.window.RunCradleGraph.lineDiff;
assert.equal(typeof diff, "function");
const changes = diff("a\nb\nc", "b\na\nc");
assert.ok(changes.left.some((v) => v.changed));
assert.ok(changes.right.some((v) => v.changed));
assert.equal(
  diff("same\nline", "same\nline").left.some((v) => v.changed),
  false,
);
// A live prerequisite is waiting for completion; its exit is not unknown yet.
assert.equal(state("process_completed_successfully", [run]).state, "waiting");
// A launch error in the downstream is not proof that the engine released the edge.
assert.equal(
  state("process_log_ready", [run, { name: "b", state: "failed" }]).state,
  "idle",
);
console.log(
  "workbench graph: 18 evidence, condition and diff assertions passed",
);
