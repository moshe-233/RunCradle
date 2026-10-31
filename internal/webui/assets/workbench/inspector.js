import { withOperation, fieldErrors, clearFieldErrors } from "./feedback.js";
import { $, node, animateOnce } from "./view.js";

// Node facts remain separate; translated labels always retain the observed raw value.
export function renderFact(el, fact) {
  const labels = {
    state: "进程状态",
    health: "健康探测",
    exit_code: "直接退出回执",
    source: "事实来源",
  };
  const translations = {
    pending: "待定",
    waiting: "等待",
    running: "运行中",
    completed: "已完成",
    failed: "失败",
    skipped: "已跳过",
    unknown: "未知",
    healthy: "健康",
    unhealthy: "异常",
  };
  if (!el.querySelector(".fact-lines")) {
    el.append(node("dl", undefined, "fact-lines"));
    const details = node("details", undefined, "fact-raw");
    details.append(node("summary", "原始字段"), node("pre"));
    el.append(details);
  }
  const dl = el.querySelector(".fact-lines");
  for (const [key, label] of Object.entries(labels)) {
    let value = dl.querySelector('[data-fact-key="' + key + '"]');
    if (!value) {
      value = node("dd");
      value.dataset.factKey = key;
      dl.append(node("dt", label), value);
    }
    const raw = fact[key],
      text =
        raw === null || raw === undefined
          ? "未知"
          : translations[raw]
            ? translations[raw] + " · " + raw
            : String(raw);
    if (value.textContent !== text) value.textContent = text;
    if (key === "state") {
      value.className = "fact-badge";
      value.dataset.state = raw || "unknown";
    }
  }
  const raw = el.querySelector(".fact-raw pre"),
    text = JSON.stringify(fact, null, 2);
  if (raw.textContent !== text) raw.textContent = text;
  let diagnostic = el.querySelector(".fact-diagnostic");
  if (fact.diagnostic) {
    if (!diagnostic) {
      diagnostic = node("div", undefined, "fact-diagnostic alert");
      diagnostic.append(node("strong"), node("pre"), node("p"));
      el.append(diagnostic);
    }
    const values = [
      "失败线索 · " + fact.diagnostic.status,
      fact.diagnostic.text || "未留存正文",
      fact.diagnostic.expires_at || "",
    ];
    [...diagnostic.children].forEach((child, i) => {
      if (child.textContent !== values[i]) child.textContent = values[i];
    });
  } else diagnostic?.remove();
}

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
  onPendingChange,
}) {
  const selectedNode = () =>
    currentDisplay()?.view.nodes.find((n) => n.id === inspectorState.selected);

  let formBaseline = null;
  const errorsByNode = new Map();
  let sectionObserver, sectionFrame;
  function stopSections() {
    sectionObserver?.disconnect();
    sectionObserver = null;
    cancelAnimationFrame(sectionFrame);
  }
  function sectionElement(name) {
    return $(name === "advanced" ? "node-advanced" : "section-" + name);
  }
  function syncSections() {
    cancelAnimationFrame(sectionFrame);
    sectionFrame = requestAnimationFrame(() => {
      if ($("inspector").hidden || $("node-config").hidden) return;
      const top = $("inspector").getBoundingClientRect().top + 64;
      let current = "basic";
      for (const button of $("inspector-sections").children)
        if (
          sectionElement(
            button.dataset.inspectorSection,
          ).getBoundingClientRect().top <= top
        )
          current = button.dataset.inspectorSection;
      for (const button of $("inspector-sections").children) {
        if (button.dataset.inspectorSection === current)
          button.setAttribute("aria-current", "location");
        else button.removeAttribute("aria-current");
      }
    });
  }
  function startSections() {
    stopSections();
    if ($("inspector").hidden || $("node-config").hidden) return;
    sectionObserver = new ResizeObserver(syncSections);
    sectionObserver.observe($("node-fields"));
    sectionObserver.observe($("inspector"));
    syncSections();
  }
  function fieldFor(key) {
    const fixed = {
      name: "node-name",
      entrypoint: "add-argument",
      command: "node-command",
      working_dir: "node-working-dir",
      label: "node-label",
      restart: "node-restart",
      exit_on_end: "node-exit-end",
      exit_on_skipped: "node-exit-skipped",
    };
    if (fixed[key]) return $(fixed[key]);
    const [kind, index, part] = key.split(".");
    if (kind === "env")
      return $("env-rows").children[index]?.querySelector(
        part === "value" ? ".env-value" : ".env-key",
      );
    if (kind === "dep")
      return $("dependency-rows").children[index]?.querySelectorAll("select")[
        part === "condition" ? 1 : 0
      ];
  }
  function localErrors() {
    const v = inspectorValues(),
      errors = [];
    const add = (key, message) => errors.push({ key, message });
    if (!v.name) add("name", "节点名称不能为空");
    else if (
      readEditor().document.display.view.nodes.some(
        (n) => n.id !== inspectorState.selected && n.name === v.name,
      )
    )
      add("name", "节点名称已存在，请使用不同名称");
    if (v.command_mode === "entrypoint" && !v.entrypoint.length)
      add("entrypoint", "参数数组至少需要一个元素");
    const keys = new Set(),
      deps = new Set();
    v.env_items.forEach((entry, index) => {
      if (!entry.key || /[=\0]/.test(entry.key))
        add(`env.${index}.key`, "环境变量名不能为空，也不能含 = 或空字符");
      else if (keys.has(entry.key)) add(`env.${index}.key`, "环境变量名重复");
      if (entry.value.includes("\0"))
        add(`env.${index}.value`, "环境变量值不能含空字符");
      keys.add(entry.key);
    });
    v.depends_on.forEach((entry, index) => {
      if (!entry.from) add(`dep.${index}.from`, "请选择前置依赖");
      else if (deps.has(entry.from)) add(`dep.${index}.from`, "前置依赖重复");
      deps.add(entry.from);
    });
    return errors;
  }
  // Paths are matched as complete strings, never split on dots in node names.
  function issueKey(
    issue,
    target = selectedNode(),
    values = inspectorValues(),
    nodes = readEditor().document.display.view.nodes,
  ) {
    if (issue.code === "syntax") return undefined;
    const n = target;
    const matches = nodes.filter((node) =>
      issue.path?.startsWith("$.processes." + node.name + "."),
    );
    if (matches.length !== 1 || matches[0].id !== n?.id) return undefined;
    for (const key of [
      "command",
      "entrypoint",
      "working_dir",
      "restart",
      "exit_on_end",
      "exit_on_skipped",
    ]) {
      const path =
        "$.processes." +
        n.name +
        "." +
        (["restart", "exit_on_end", "exit_on_skipped"].includes(key)
          ? "availability."
          : "") +
        key;
      if (issue.path === path) return key;
    }
    const paths = [];
    values.depends_on.forEach((dep, index) => {
      paths.push([
        "$.processes." + n.name + ".depends_on." + dep.from,
        `dep.${index}.from`,
      ]);
      paths.push([
        "$.processes." + n.name + ".depends_on." + dep.from + ".condition",
        `dep.${index}.condition`,
      ]);
    });
    const keys = paths.filter(([path]) => path === issue.path);
    return keys.length === 1 ? keys[0][1] : undefined;
  }
  function displayErrors(record, focus = false, announce = true) {
    fieldErrors(
      $("node-config"),
      $("inspector-error"),
      (record?.errors || []).map((error) => ({
        field:
          record?.server && record.modified
            ? undefined
            : fieldFor(error.key || ""),
        message:
          error.message +
          (record?.modified && record.server ? "（已修改，待重新校验）" : ""),
      })),
      {
        focus,
        announce,
        source: record?.server ? () => showDialog("source-dialog") : undefined,
      },
    );
  }
  function editErrors() {
    const n = selectedNode(),
      record = errorsByNode.get(n?.id);
    if (!record) return;
    if (record.server) record.modified = true;
    else
      record.errors = localErrors().filter((error) =>
        record.errors.some((old) => old.key === error.key),
      );
    displayErrors(record, false, false);
  }
  // Compare only editable declarations, not graph-derived reachability.
  const signature = (n) =>
    JSON.stringify([
      n.name,
      n.label,
      n.command_mode,
      n.declaration_text,
      n.working_dir,
      n.entrypoint_items,
      n.env_items,
      n.dependencies,
      n.restart,
      n.exit_on_end,
      n.exit_on_skipped,
    ]);
  function pendingNodes() {
    rememberInspector();
    const nodes = readEditor().document?.display.view.nodes || [];
    return nodes.filter((n) => {
      const cached = inspectorState.inspectorDrafts.get(n.id);
      return (
        cached?.signature === signature(n) &&
        JSON.stringify(cached.values) !== JSON.stringify(cached.baseline)
      );
    });
  }
  function changed() {
    const pending = pendingNodes();
    $("inspector-pending").hidden = !pending.some(
      (n) => n.id === inspectorState.selected,
    );
    onPendingChange();
  }

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
    if (opening) {
      const mobile = matchMedia("(max-width:700px)").matches;
      const target = inspectorOverlay()
        ? $("inspector")
        : $(readView().mode === "design" ? "node-config" : "node-facts");
      animateOnce(
        target,
        [
          {
            opacity: 0,
            translate: mobile
              ? "0 16px"
              : inspectorOverlay()
                ? "24px 0"
                : "12px 0",
          },
          { opacity: 1, translate: "0 0" },
        ],
        280,
      );
    }
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
    stopSections();
    currentCanvas()
      .querySelector(`.graph-node[data-node="${inspectorState.selected}"]`)
      ?.focus({ preventScroll: true });
  }

  function inspectorTab(tab) {
    const panel = $(tab === "config" ? "node-config" : "node-facts");
    const changed = panel.hidden;
    $("node-config").hidden = tab !== "config";
    $("node-facts").hidden = tab !== "facts";
    $("config-tab").setAttribute("aria-pressed", String(tab === "config"));
    $("facts-tab").setAttribute("aria-pressed", String(tab === "facts"));
    if (changed) animateOnce(panel, [{ opacity: 0 }, { opacity: 1 }]);
    startSections();
  }

  function renderNodeFacts() {
    const n = selectedNode(),
      fact = readRuns().snapshot?.nodes?.find((f) => f.name === n?.name),
      el = $("node-facts");
    const identity = readRuns().snapshot?.run_id + ":" + n?.name;
    if (el.dataset.identity !== identity) {
      el.replaceChildren();
      el.dataset.identity = identity;
    }
    if (!fact) {
      el.replaceChildren(node("p", "尚无该节点的运行事实。"));
      return;
    }
    if (
      readView().mode !== "monitor" &&
      readEditor().document?.revision !== readRuns().snapshot?.revision
    ) {
      el.replaceChildren(
        node("p", "所选运行属于另一个版本，请切换实时监控查看。"),
      );
      return;
    }
    if (!el.querySelector(".fact-lines")) el.replaceChildren();
    renderFact(el, fact);
  }

  function textInput(value, label, cls) {
    const e = node("input");
    e.value = value || "";
    e.setAttribute("aria-label", label);
    if (cls) e.className = cls;
    return e;
  }

  function removeButton(row) {
    const b = node("button");
    b.innerHTML =
      '<svg class="control-icon" viewBox="0 0 20 20" aria-hidden="true"><path d="m6 6 8 8M14 6l-8 8"/></svg>';
    b.setAttribute("aria-label", "删除此行");
    b.addEventListener("click", () => {
      row.remove();
      editErrors();
      changed();
    });
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
        e.bound_input ? e.bound_input : "绑定参数",
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
        inspectorState.binding = {
          process: selectedNode().name,
          key: key.value,
        };
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
        from: row.querySelectorAll("select")[0].value,
        condition: row.querySelectorAll("select")[1].value,
      })),
      restart: $("node-restart").value,
      exit_on_end: $("node-exit-end").checked,
      exit_on_skipped: $("node-exit-skipped").checked,
    };
  }

  function rememberInspector() {
    // 缓存尚未应用的表单输入；关闭或切换节点不代表用户要求丢弃。
    const n = selectedNode();
    if (
      readView().mode !== "design" ||
      !n ||
      $("inspector").hidden ||
      formBaseline?.id !== n.id ||
      formBaseline.signature !== signature(n)
    )
      return;
    inspectorState.inspectorDrafts.set(n.id, {
      signature: signature(n),
      baseline: formBaseline.values,
      values: inspectorValues(),
    });
  }

  function fillInspector(n) {
    const cached = inspectorState.inspectorDrafts.get(n.id);
    const v = cached?.signature === signature(n) ? cached.values : null;
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
    formBaseline = {
      id: n.id,
      signature: signature(n),
      values: v ? cached.baseline : inspectorValues(),
    };
    const record = errorsByNode.get(n.id);
    if (record?.signature !== signature(n)) errorsByNode.delete(n.id);
    displayErrors(errorsByNode.get(n.id), false, false);
    changed();
    startSections();
  }

  function commandMode() {
    const entry = $("command-mode").value === "entrypoint";
    $("entrypoint-editor").hidden = !entry;
    $("command-label").hidden = entry;
  }

  async function applyNode() {
    const n = selectedNode();
    if (!n) throw new Error("请选择节点");
    const local = localErrors();
    if (local.length) {
      const record = { signature: signature(n), errors: local };
      errorsByNode.set(n.id, record);
      displayErrors(record, true);
      throw new Error("请修正节点字段错误；输入已保留。");
    }
    clearFieldErrors($("node-config"));
    $("inspector-error").hidden = true;
    const submitted = inspectorValues(),
      nodes = readEditor().document.display.view.nodes;
    const documentAtSubmit = readEditor().document;
    try {
      const data = await applyNodeValues();
      errorsByNode.delete(n.id);
      return data;
    } catch (error) {
      const issues = error.data?.issues?.filter(
        (issue) => issue.severity === "error",
      );
      const record = {
        signature: signature(n),
        server: true,
        errors: issues?.length
          ? issues.map((issue) => ({
              key: issueKey(issue, n, submitted, nodes),
              message:
                issue.message +
                (issue.path ? ` · ${issue.path}` : "") +
                (issue.line ? ` · 行 ${issue.line}` : ""),
            }))
          : [{ message: error.message }],
      };
      if (readEditor().document !== documentAtSubmit) throw error;
      errorsByNode.set(n.id, record);
      if (selectedNode()?.id === n.id) {
        record.modified =
          JSON.stringify(inspectorValues()) !== JSON.stringify(submitted);
        displayErrors(record, true);
      }
      changed();
      throw error;
    }
  }

  async function applyNodeValues() {
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
    fieldErrors($("add-dialog"), $("add-error"), []);
    showDialog("add-dialog");
  }

  function resetInspectorDrafts() {
    inspectorState.inspectorDrafts.clear();
    errorsByNode.clear();
    stopSections();
    formBaseline = null;
  }

  function clearSelection() {
    inspectorState.selected = "";
  }

  function readInspector() {
    return Object.freeze({ ...inspectorState });
  }
  let bound = false;
  function bindEvents() {
    if (bound) return;
    bound = true;
    $("node-fields").addEventListener("input", editErrors);
    $("node-fields").addEventListener("change", editErrors);
    $("inspector").addEventListener("scroll", syncSections);
    $("node-advanced").addEventListener("toggle", syncSections);
    for (const button of $("inspector-sections").children) {
      button.addEventListener("mousedown", (event) => event.preventDefault());
      button.addEventListener("click", () => {
        const section = sectionElement(button.dataset.inspectorSection);
        if (section.matches("details")) section.open = true;
        const host = $("inspector"),
          top =
            section.getBoundingClientRect().top -
            host.getBoundingClientRect().top +
            host.scrollTop -
            56;
        host.scrollTo({
          top: Math.max(0, top),
          behavior: matchMedia("(prefers-reduced-motion:reduce)").matches
            ? "instant"
            : "smooth",
        });
      });
    }
    const visibility = new MutationObserver(() => {
      if ($("inspector").hidden) stopSections();
      else startSections();
    });
    visibility.observe($("inspector"), {
      attributes: true,
      attributeFilter: ["hidden"],
    });
    window.addEventListener("pagehide", stopSections);
    window.addEventListener("pageshow", startSections);
    $("node-fields").addEventListener("input", changed);
    $("node-fields").addEventListener("change", changed);
    on("close-inspector", closeInspector);
    on("discard-node", () => {
      const n = selectedNode();
      if (!n) return;
      inspectorState.inspectorDrafts.delete(n.id);
      errorsByNode.delete(n.id);
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
        const message = !name
          ? "节点名称不能为空"
          : readEditor().document.display.view.nodes.some(
                (node) => node.name === name,
              )
            ? "节点名称已存在，请使用不同名称"
            : "";
        fieldErrors(
          $("add-dialog"),
          $("add-error"),
          message ? [{ field: $("add-name"), message }] : [],
          { focus: true },
        );
        if (message) return;
        await applyWorkbenchEdit({
          action: "add_process",
          process: name,
          upstream: inspectorState.addUpstream,
        });
        closeDialog("add-dialog");
      },
      true,
    );
    on(
      "apply-node",
      () =>
        withOperation("apply-node", "应用中…", async (succeeded) => {
          const data = await applyNode();
          if (data?.valid) succeeded("已应用");
        }),
      true,
    );
    on(
      "delete-node",
      async () => {
        await applyWorkbenchEdit({
          action: "delete_process",
          process: selectedNode().name,
        });
        $("inspector").hidden = true;
      },
      true,
    );
    on("add-argument", () => {
      argumentRow();
      changed();
    });
    on("add-env", () => {
      envRow();
      changed();
    });
    on("add-dependency", () => {
      dependencyRow();
      changed();
    });
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
        const errors = [];
        for (const [field, entries] of [
          [$("bind-default"), [$("bind-default").value]],
          [$("bind-enum"), values ? values.split("\n") : []],
        ]) {
          try {
            for (const value of entries) typedRaw(type, value);
          } catch (error) {
            errors.push({ field, message: error.message });
          }
        }
        fieldErrors($("bind-dialog"), $("bind-error"), errors, { focus: true });
        if (errors.length) return;
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
    pendingNodes,
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
