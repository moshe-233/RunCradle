import { $, node } from "./view.js";

// 检查器保存未应用输入；关闭不等于丢弃，只有显式操作才清除。
export function createInspector({
  inspectorState,
  api,
  closeDialog,
  currentCanvas,
  currentDisplay,
  draft,
  guard,
  on,
  readEditor,
  readRuns,
  readView,
  selectNode,
  showDialog,
  typedRaw,
  updateCanvas,
  applyWorkbenchEdit,
}) {
  const selectedNode = () =>
    currentDisplay()?.view.nodes.find((n) => n.id === inspectorState.selected);

  const conditionNames = {
    process_started: "启动后",
    process_completed: "结束后",
    process_completed_successfully: "成功完成后",
    process_healthy: "健康就绪后",
    process_log_ready: "日志就绪后",
  };

  function selectWorkbenchNode(id, remember = true) {
    if (remember) rememberInspector();
    const opening = $("inspector").hidden;
    inspectorState.selected = id;
    const n = selectedNode();
    if (!n) return;
    $("inspector").hidden = false;
    $("inspector-title").textContent = n.label;
    $("inspector-id").textContent = n.name;
    $("config-tab").disabled = readView().mode === "monitor";
    inspectorTab(readView().mode === "monitor" ? "facts" : "config");
    if (readView().mode === "design") {
      selectNode(id);
      fillInspector(n);
    }
    renderNodeFacts();
    updateCanvas();
    inspectorPresentation(opening);
  }

  const inspectorOverlay = () => matchMedia("(max-width: 1250px)").matches;

  function inspectorPresentation(opening = false) {
    $("inspector").setAttribute(
      "role",
      inspectorOverlay() ? "dialog" : "complementary",
    );
    $("inspector").setAttribute("aria-labelledby", "inspector-title");
    if (opening && inspectorOverlay()) $("close-inspector").focus();
  }

  function closeInspector() {
    rememberInspector();
    $("inspector").hidden = true;
    currentCanvas()
      .querySelector(`.graph-node[data-node="${inspectorState.selected}"]`)
      ?.focus({ preventScroll: true });
  }

  function inspectorTab(tab) {
    $("node-config").hidden = tab !== "config";
    $("node-facts").hidden = tab !== "facts";
    $("config-tab").setAttribute("aria-pressed", String(tab === "config"));
    $("facts-tab").setAttribute("aria-pressed", String(tab === "facts"));
  }

  function renderNodeFacts() {
    const n = selectedNode(),
      fact = readRuns().snapshot?.nodes?.find((f) => f.name === n?.name),
      el = $("node-facts");
    el.replaceChildren();
    if (!fact) {
      el.append(node("p", "尚无该节点的运行事实。"));
      return;
    }
    if (
      readView().mode !== "monitor" &&
      readEditor().document?.revision !== readRuns().snapshot?.revision
    ) {
      el.append(node("p", "所选运行属于另一个版本，请切换实时监控查看。"));
      return;
    }
    if (fact.diagnostic) {
      const alert = node("div", undefined, "alert");
      alert.append(
        node("strong", `失败线索 · ${fact.diagnostic.status}`),
        node("pre", fact.diagnostic.text || "未留存正文"),
        node("p", fact.diagnostic.expires_at || ""),
      );
      el.append(alert);
    }
    const dl = node("dl");
    for (const key of [
      "state",
      "raw_state",
      "health",
      "exit_code",
      "signal",
      "incarnation",
      "upstream_node_exit_code",
      "started_at",
      "ended_at",
      "source",
    ]) {
      dl.append(
        node("dt", key),
        node(
          "dd",
          fact[key] === undefined || fact[key] === null
            ? "未知"
            : String(fact[key]),
        ),
      );
    }
    el.append(dl);
  }

  function textInput(value, label, cls) {
    const e = node("input");
    e.value = value || "";
    e.setAttribute("aria-label", label);
    if (cls) e.className = cls;
    return e;
  }

  function removeButton(row) {
    const b = node("button", "×");
    b.setAttribute("aria-label", "删除此行");
    b.addEventListener("click", () => row.remove());
    return b;
  }

  function argumentRow(value = "") {
    const row = node("div", undefined, "arg-row");
    row.append(textInput(value, "命令参数"), removeButton(row));
    $("entrypoint-rows").append(row);
  }

  function envRow(e = { key: "", value: "" }) {
    const row = node("div", undefined, "kv-row"),
      key = textInput(e.key, "环境变量名", "env-key"),
      value = textInput(e.value, "环境变量值", "env-value"),
      bind = node(
        "button",
        e.bound_input ? "◇ " + e.bound_input : "⚡ 参数",
        e.bound_input ? "bound-param" : "",
      );
    bind.setAttribute(
      "aria-label",
      e.bound_input ? "解除参数绑定 " + e.bound_input : "绑定运行参数",
    );
    bind.addEventListener(
      "click",
      guard(async () => {
        const n = selectedNode();
        if (e.bound_input) {
          await applyWorkbenchEdit({
            action: "unbind_input",
            process: n.name,
            input_name: e.bound_input,
            env_key: e.key,
          });
          return;
        }
        if (!key.value) throw new Error("先填写变量名");
        await applyNode();
        inspectorState.binding = { process: selectedNode().name, key: key.value };
        $("bind-name").value = key.value.toLowerCase();
        $("bind-type").value = "string";
        $("bind-default").value = value.value;
        $("bind-enum").value = "";
        showDialog("bind-dialog");
      }),
    );
    row.dataset.boundInput = e.bound_input || "";
    row.append(key, value, bind, removeButton(row));
    $("env-rows").append(row);
  }

  function dependencyRow(d = {}) {
    const row = node("div", undefined, "dep-row"),
      from = node("select"),
      condition = node("select");
    from.setAttribute("aria-label", "前置节点");
    condition.setAttribute("aria-label", "依赖条件");
    for (const n of readEditor().document.display.view.nodes)
      if (n.name !== selectedNode()?.name)
        from.append(new Option(n.label, n.name));
    for (const [k, v] of Object.entries(conditionNames))
      condition.append(new Option(v, k));
    if (d.from) from.value = d.from;
    condition.value =
      d.condition === "未显式指定"
        ? "process_started"
        : d.condition || "process_completed_successfully";
    row.append(from, condition, removeButton(row));
    $("dependency-rows").append(row);
  }

  function inspectorValues() {
    return {
      name: $("node-name").value,
      label: $("node-label").value,
      command_mode: $("command-mode").value,
      command: $("node-command").value,
      entrypoint: [...$("entrypoint-rows").querySelectorAll("input")].map(
        (e) => e.value,
      ),
      working_dir: $("node-working-dir").value,
      env_items: [...$("env-rows").children].map((row) => ({
        key: row.querySelector(".env-key").value,
        value: row.querySelector(".env-value").value,
        bound_input: row.dataset.boundInput,
      })),
      depends_on: [...$("dependency-rows").children].map((row) => ({
        from: row.children[0].value,
        condition: row.children[1].value,
      })),
      restart: $("node-restart").value,
      exit_on_end: $("node-exit-end").checked,
      exit_on_skipped: $("node-exit-skipped").checked,
    };
  }

  function rememberInspector() {
  // 缓存尚未应用的表单输入；关闭或切换节点不代表用户要求丢弃。
    const n = selectedNode();
    if (readView().mode !== "design" || !n || $("inspector").hidden) return;
    inspectorState.inspectorDrafts.set(n.id, {
      signature: JSON.stringify(n),
      values: inspectorValues(),
    });
  }

  function fillInspector(n) {
    const cached = inspectorState.inspectorDrafts.get(n.id);
    const v = cached?.signature === JSON.stringify(n) ? cached.values : null;
    if (!v) inspectorState.inspectorDrafts.delete(n.id);
    $("node-name").value = v?.name ?? n.name;
    $("node-label").value = v?.label ?? n.label;
    $("command-mode").value =
      (v?.command_mode ?? n.command_mode) === "entrypoint"
        ? "entrypoint"
        : "command";
    $("node-command").value = v?.command ?? n.declaration_text.command ?? "";
    $("node-working-dir").value = v?.working_dir ?? n.working_dir;
    $("node-restart").value = v?.restart ?? n.restart;
    $("node-exit-end").checked = v?.exit_on_end ?? n.exit_on_end;
    $("node-exit-skipped").checked = v?.exit_on_skipped ?? n.exit_on_skipped;
    $("entrypoint-rows").replaceChildren();
    (v?.entrypoint ?? n.entrypoint_items).forEach(argumentRow);
    $("env-rows").replaceChildren();
    (v?.env_items ?? n.env_items).forEach(envRow);
    $("dependency-rows").replaceChildren();
    (v?.depends_on ?? n.dependencies).forEach(dependencyRow);
    const raw = readEditor().document.display.view.raw_edit_only;
    $("raw-only").hidden = !raw;
    $("node-fields").disabled = raw || !readEditor().document.valid;
    commandMode();
  }

  function commandMode() {
    const entry = $("command-mode").value === "entrypoint";
    $("entrypoint-editor").hidden = !entry;
    $("command-label").hidden = entry;
  }

  async function applyNode() {
    const n = selectedNode();
    if (!n) throw new Error("请选择节点");
    const { name, ...values } = inspectorValues();
    values.env_items = values.env_items.map(({ key, value }) => ({
      key,
      value,
    }));
    let source = draft();
    if (name !== n.name) {
      // Both operations validate in memory; render only after the whole edit succeeds.
      const renamed = await api("validate", {
        method: "POST",
        body: {
          ...source,
          workbench_edit: {
            action: "rename_process",
            process: n.name,
            new_name: name,
          },
        },
      });
      source = {
        format: renamed.format,
        source: renamed.source,
        entry: renamed.entry,
      };
      if (renamed.ui_source)
        Object.assign(source, {
          ui_source: renamed.ui_source,
          ui_path: renamed.ui_path,
        });
    }
    return applyWorkbenchEdit(
      { action: "update_process", process: name, ...values },
      {},
      source,
    );
  }

  function addNode(upstream = "") {
    inspectorState.addUpstream = upstream;
    $("add-upstream").textContent = upstream
      ? `前置 ${upstream} · 成功完成后启动`
      : "独立节点";
    $("add-name").value = "";
    showDialog("add-dialog");
  }

  function resetInspectorDrafts() { inspectorState.inspectorDrafts.clear(); }

  function clearSelection() { inspectorState.selected = ""; }

  function readInspector() {
    return Object.freeze({ ...inspectorState });
  }
  let bound = false;
  function bindEvents() {
    if (bound) return;
    bound = true;
    on("close-inspector", closeInspector);
    on("discard-node", () => {
      const n = selectedNode();
      if (!n) return;
      inspectorState.inspectorDrafts.delete(n.id);
      fillInspector(n);
    });
    on("config-tab", () => inspectorTab("config"));
    on("facts-tab", () => {
      inspectorTab("facts");
      renderNodeFacts();
    });
    on("add-node", () => addNode());
    on(
      "confirm-add",
      async () => {
        const name = $("add-name").value;
        await applyWorkbenchEdit({
          action: "add_process",
          process: name,
          upstream: inspectorState.addUpstream,
        });
        closeDialog("add-dialog");
      },
      true,
    );
    on("apply-node", applyNode, true);
    on(
      "delete-node",
      async () => {
        await applyWorkbenchEdit({ action: "delete_process", process: selectedNode().name });
        $("inspector").hidden = true;
      },
      true,
    );
    on("add-argument", () => argumentRow());
    on("add-env", () => envRow());
    on("add-dependency", () => dependencyRow());
    $("command-mode").addEventListener("change", commandMode);
    on(
      "confirm-bind",
      async () => {
        const type = $("bind-type").value,
          spec = {
            name: $("bind-name").value,
            type,
            bindings: [{ ...inspectorState.binding, field: "environment" }],
          };
        const values = $("bind-enum").value.trim();
        let raw =
          JSON.stringify(spec).slice(0, -1) +
          ',"default":' +
          typedRaw(type, $("bind-default").value);
        if (values)
          raw +=
            ',"enum":[' +
            values
              .split("\n")
              .map((v) => typedRaw(type, v))
              .join(",") +
            "]";
        raw += "}";
        await applyWorkbenchEdit({ action: "bind_input" }, { input_spec: raw });
        closeDialog("bind-dialog");
      },
      true,
    );
  }
  return {
    clearSelection,
    rememberInspector,
    resetInspectorDrafts,
    selectWorkbenchNode,
    selectedNode,
    renderNodeFacts,
    addNode,
    closeInspector,
    inspectorOverlay,
    inspectorPresentation,
    read: readInspector,
    bindEvents,
  };
}
