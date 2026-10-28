"use strict";
(() => {
  const $ = (id) => document.getElementById(id);
  const node = (tag, text, cls) => {
    const e = document.createElement(tag);
    if (text !== undefined) e.textContent = text;
    if (cls) e.className = cls;
    return e;
  };
  let editorBusy = false,
    disabledBefore = [];
  const state = {
    document: null,
    workflow: null,
    expected: "",
    savedRevision: "",
    committed: null,
    stream: null,
    activeRun: "",
    workflowOffset: 0,
    runOffset: 0,
    gap: false,
    refreshTimer: null,
  };
  const normalize = (text) => text.replace(/\r\n?/g, "\n");
  function notice(message, error = false) {
    $("notice").hidden = false;
    $("notice").textContent = message;
    $("notice").classList.toggle("error", error);
  }
  async function api(path, options = {}) {
    const init = {
      method: options.method || "GET",
      credentials: "same-origin",
      cache: "no-store",
      ...options,
    };
    if (init.body !== undefined) {
      init.headers = {
        "Content-Type": "application/json",
        ...(init.headers || {}),
      };
      if (typeof init.body !== "string") init.body = JSON.stringify(init.body);
    }
    let response;
    try {
      response = await fetch("/api/v1/" + path, init);
    } catch (e) {
      $("connection-state").textContent = "连接已断开";
      throw e;
    }
    const data = await response.json();
    if (!response.ok) {
      const e = new Error(data.error || `请求失败 ${response.status}`);
      e.status = response.status;
      e.data = data;
      throw e;
    }
    $("connection-state").textContent = "已连接";
    return data;
  }
  function draft() {
    const old = state.document;
    const source =
      old && normalize(old.source) === $("native-source").value
        ? old.source
        : $("native-source").value;
    const result = {
      format: $("source-format").value,
      source,
      entry: old?.entry || "process-compose." + $("source-format").value,
    };
    if ($("ui-source").value.trim()) {
      result.ui_source =
        old && normalize(old.ui_source || "") === $("ui-source").value
          ? old.ui_source
          : $("ui-source").value;
      result.ui_path = old?.ui_path || "ui.json";
    }
    return result;
  }
  const sameDraft = (a, b) =>
    b &&
    a.format === b.format &&
    a.source === b.source &&
    (a.ui_source || "") === (b.ui_source || "") &&
    a.entry === b.entry;
  function dirty() {
    return !sameDraft(draft(), state.committed);
  }
  function syncButtons() {
    $("draft-status").textContent = dirty()
      ? "草稿已修改 · 未保存"
      : "已保存 · 运行固定此版本";
    $("start-run").disabled =
      editorBusy ||
      !state.workflow ||
      dirty() ||
      !state.document?.valid ||
      !state.document?.run_supported;
    $("copy-workflow").disabled = !state.workflow;
    $("export-workflow").disabled = !state.workflow;
    $("reimport-workspace").disabled = !state.workflow;
  }
  function markDirty() {
    syncButtons();
    $("execution-note").textContent =
      "草稿修改后需要校验并保存；当前运行仍使用原版本。";
  }
  function drawGraph(container, display, select, facts = []) {
    RunCradleGraph.draw(
      container,
      display,
      selectWorkbenchNode,
      facts,
      container.id === "editor-graph" ? { run: launch, add: addNode } : {},
    );
    updateCanvas();
  }
  function fieldValue() {
    const selected = state.document?.display?.view.nodes.find(
      (n) => n.name === $("edit-node").value,
    );
    const field = $("edit-field").value;
    let value = selected?.declaration_text[field];
    if (value === undefined)
      value = ["environment", "entrypoint"].includes(field)
        ? "[]"
        : field === "depends_on"
          ? "{}"
          : "";
    $("field-value").value = value;
  }
  function selectNode(id) {
    const selected = state.document?.display?.view.nodes.find(
      (n) => n.id === id,
    );
    if (!selected) return;
    $("edit-node").value = selected.name;
    if (
      $("edit-field").value === "command" &&
      selected.declaration_text.command === undefined &&
      selected.declaration_text.entrypoint !== undefined
    )
      $("edit-field").value = "entrypoint";
    $("declaration-detail").textContent = [
      selected.label,
      selected.name,
      ...Object.entries(selected.declaration_text).map(
        ([key, value]) => `${key}\n${value}`,
      ),
    ].join("\n\n");
    $("step-list")
      .querySelectorAll("button")
      .forEach((e) => {
        e.classList.toggle("selected", e.dataset.node === id);
        e.setAttribute("aria-current", String(e.dataset.node === id));
      });
    fieldValue();
  }
  function renderDocument(data, { saved = false, fresh = false } = {}) {
    if (fresh) mode("design");
    $("reimport-preview").hidden = true;
    state.importPreview = null;
    if (fresh) {
      state.workflow = null;
      state.expected = "";
      state.savedRevision = "";
      state.committed = null;
      $("conflict").hidden = true;
    }
    state.document = data;
    $("source-format").value = data.format;
    $("native-source").value = data.source;
    $("ui-source").value = data.ui_source || "";
    $("workflow-title").textContent = data.display.view.title || "工作流";
    if (saved) {
      state.workflow = data.workflow;
      state.expected = data.workflow.revision;
      state.savedRevision = data.revision;
      state.committed = {
        format: data.format,
        source: data.source,
        entry: data.entry,
        ui_source: data.ui_source,
        ui_path: data.ui_path,
      };
      $("conflict").hidden = true;
    }
    $("workflow-id").textContent = state.workflow?.workflow_id || "";
    $("saved-revision").textContent = state.savedRevision;
    $("version-select").replaceChildren();
    $("version-select").disabled = !state.workflow;
    if (state.workflow) {
      for (const revision of [...state.workflow.revisions].reverse())
        $("version-select").append(new Option(revision.slice(0, 12), revision));
      $("version-select").value = state.savedRevision;
    }
    $("issues").replaceChildren();
    for (const issue of data.issues || []) {
      const li = node("li"),
        b = node(
          "button",
          `${issue.severity} · ${issue.path || "$"} ${issue.message}`,
        );
      b.addEventListener("click", () => {
        const area = issue.path?.startsWith("ui")
          ? $("ui-source")
          : $("native-source");
        const lines = area.value.split("\n"),
          line = Math.max(0, (issue.line || 1) - 1),
          start = lines.slice(0, line).reduce((n, s) => n + s.length + 1, 0);
        area.focus();
        area.setSelectionRange(start, start + (lines[line]?.length || 0));
        area.scrollTop = line * 18;
      });
      li.append(b);
      $("issues").append(li);
    }
    $("validate-draft").textContent = (data.issues || []).some(
      (i) => i.severity === "error",
    )
      ? `校验 · ${(data.issues || []).filter((i) => i.severity === "error").length} 问题`
      : "✓ 校验";
    $("execution-note").textContent = data.execution_note;
    const previous = $("edit-node").value;
    $("edit-node").replaceChildren();
    $("run-targets").replaceChildren();
    $("step-list").replaceChildren();
    for (const item of data.display.view.nodes) {
      $("edit-node").append(new Option(item.label, item.name));
      $("run-targets").append(new Option(item.name, item.name));
      const button = node("button", item.label);
      button.dataset.node = item.id;
      button.addEventListener("click", () => selectNode(item.id));
      $("step-list").append(button);
    }
    if (data.display.view.nodes.length) {
      $("run-targets").options[$("run-targets").options.length - 1].selected =
        true;
      const initial =
        data.display.view.nodes.find((n) => n.name === previous) ||
        data.display.view.nodes[0];
      selectNode(initial.id);
    } else $("declaration-detail").textContent = "暂无节点";
    $("apply-field").disabled = !data.valid || data.display.view.raw_edit_only;
    $("remove-field").disabled = $("apply-field").disabled;
    drawGraph($("editor-graph"), data.display, selectNode);
    renderParameters(data.display);
    syncButtons();
    sourceLines();
    updateCanvas();
    if (state.mode === "design" && !$("inspector").hidden) {
      const selected = data.display.view.nodes.find(
        (n) => n.id === state.selected,
      );
      if (selected) selectWorkbenchNode(selected.id);
      else {
        $("inspector").hidden = true;
        state.selected = "";
      }
    }
  }
  async function validate(extra = {}) {
    const data = await api("validate", {
      method: "POST",
      body: { ...draft(), ...extra },
    });
    renderDocument(data);
    return data;
  }
  async function library(more = false) {
    if (!more) {
      state.workflowOffset = 0;
      $("workflow-list").replaceChildren();
    }
    const data = await api(`workflows?offset=${state.workflowOffset}&limit=20`);
    state.workflowOffset = data.next_offset;
    $("more-workflows").hidden = !data.has_more;
    for (const item of data.workflows) {
      const button = node(
        "button",
        `${item.title} · ${item.revision.slice(0, 8)}`,
      );
      button.dataset.workflow = item.workflow_id;
      button.addEventListener(
        "click",
        guard(() => openWorkflow(item.workflow_id)),
      );
      $("workflow-list").append(button);
    }
  }
  async function openWorkflow(id, revision = "") {
    const data = await api(
      `workflows/${encodeURIComponent(id)}${revision ? "?revision=" + encodeURIComponent(revision) : ""}`,
    );
    renderDocument(data, { saved: true, fresh: true });
    history.replaceState(null, "", `/?workflow=${encodeURIComponent(id)}`);
    await runHistory();
  }
  async function save() {
    const data = await validate();
    if (!data.valid) {
      notice("请先修正校验错误。", true);
      return;
    }
    try {
      const id = state.workflow?.workflow_id;
      const saved = await api(id ? `workflows/${id}` : "workflows", {
        method: id ? "PUT" : "POST",
        body: {
          ...draft(),
          ...(id ? { expected_revision: state.expected } : {}),
        },
      });
      renderDocument(saved, { saved: true });
      history.replaceState(
        null,
        "",
        `/?workflow=${saved.workflow.workflow_id}` +
          (state.activeRun ? "#run=" + state.activeRun : ""),
      );
      notice("版本已保存，导入源文件未修改。");
      await library();
      await runHistory();
    } catch (e) {
      if (e.status === 409 && state.workflow) {
        const latest = await api(`workflows/${state.workflow.workflow_id}`);
        $("conflict-source").textContent =
          `最新版本 ${latest.revision}\n${latest.source}\n${latest.ui_source || ""}`;
        $("conflict").hidden = false;
        showDiff(
          "保存冲突 · 草稿保留",
          draft().source + "\n" + (draft().ui_source || ""),
          latest.source + "\n" + (latest.ui_source || ""),
        );
        notice("保存冲突：草稿保留，请比较最新版本。", true);
      } else throw e;
    }
  }
  async function fresh() {
    const data = await api("validate", {
      method: "POST",
      body: {
        format: "yaml",
        source:
          'version: "0.5"\nprocesses:\n  check:\n    command: echo ready\n    availability:\n      exit_on_end: true\n',
      },
    });
    renderDocument(data, { fresh: true });
    history.replaceState(null, "", "/");
  }
  async function edit(remove = false) {
    const field = $("edit-field").value;
    const edit = { process: $("edit-node").value, field, remove };
    let raw;
    if (remove) raw = JSON.stringify({ ...draft(), edit });
    else {
      let value;
      if (["command", "working_dir"].includes(field))
        value = JSON.stringify($("field-value").value);
      else {
        value = $("field-value").value;
        JSON.parse(value);
      }
      const base = JSON.stringify(draft());
      raw =
        base.slice(0, -1) +
        ',"edit":' +
        JSON.stringify(edit).slice(0, -1) +
        ',"value":' +
        value +
        "}}";
    }
    const result = await api("validate", { method: "POST", body: raw });
    renderDocument(result);
    notice("字段已应用到草稿；保存后才能运行新版本。");
  }
  function closeStream() {
    if (state.stream) state.stream.close();
    state.stream = null;
    if (state.refreshTimer) clearTimeout(state.refreshTimer);
    state.refreshTimer = null;
  }
  function facts(snapshot) {
    if (snapshot.run_id !== state.activeRun) return;
    state.snapshot = snapshot;
    state.renderedRun = snapshot.run_id;
    $("run-hud").textContent =
      `${snapshot.run_id.slice(0, 8)} · ${snapshot.task_result} · ${(snapshot.nodes || []).filter((n) => n.ended_at).length}/${snapshot.nodes?.length || 0} 已结束`;
    renderNodeFacts();
    $("run-empty").hidden = true;
    $("run-id").textContent = snapshot.run_id;
    $("run-revision").textContent = snapshot.revision;
    $("run-status").textContent = snapshot.task_result || "unknown";
    $("run-engine").textContent =
      snapshot.engine_exit_code === null
        ? `未知${snapshot.engine_signal ? " · signal " + snapshot.engine_signal : ""}`
        : String(snapshot.engine_exit_code);
    $("run-integrity").textContent = state.gap
      ? `${snapshot.history_integrity} · 连接或游标缺口已观察到`
      : snapshot.history_integrity;
    $("run-cleanup").textContent = snapshot.cleanup;
    $("run-scope").textContent = snapshot.cleanup_scope || "未知";
    $("run-io").textContent = snapshot.io_status;
    $("run-reason").textContent = snapshot.reason;
    $("stop-run").disabled = snapshot.final || snapshot.owned === false;
    $("run-values").textContent =
      snapshot.values_text || "旧记录未提供实例值文本。";
    $("run-nodes").replaceChildren();
    for (const n of snapshot.nodes || []) {
      const item = node("div", undefined, "node-fact");
      item.append(
        node("strong", n.name),
        node(
          "div",
          `${n.state} · health ${n.health} · 实际退出 ${n.exit_code === null ? "未知" : n.exit_code}${n.signal ? " · signal " + n.signal : ""} · 来源 ${n.source}`,
        ),
      );
      if (n.diagnostic)
        item.append(
          node(
            "pre",
            `${n.diagnostic.status} · ${n.diagnostic.text || "正文未留存或不可用"}`,
          ),
        );
      $("run-nodes").append(item);
    }
    if (snapshot.operation_error)
      $("run-nodes").append(
        node("p", `操作错误：${snapshot.operation_error}`, "muted"),
      );
    if (snapshot.residual_path)
      $("run-nodes").append(
        node("p", `残留现场：${snapshot.residual_path}`, "muted"),
      );
    if (state.runDefinition)
      drawGraph(
        $("run-graph"),
        state.runDefinition.display,
        null,
        snapshot.nodes || [],
      );
  }
  async function refreshRun(id) {
    const snapshot = await api(`runs/${id}`);
    if (state.activeRun !== id) return;
    facts(snapshot);
    if (snapshot.final) {
      closeStream();
      $("stream-state").textContent = "终态已保存；历史完整性仍按记录显示。";
      await runHistory();
    }
  }
  async function openRun(id) {
    state.renderedRun = "";
    $("stop-run").disabled = true;
    $("run-id").textContent = "";
    $("run-revision").textContent = "";
    $("run-status").textContent = "读取中";
    for (const key of [
      "run-engine",
      "run-integrity",
      "run-cleanup",
      "run-scope",
      "run-io",
      "run-reason",
    ])
      $(key).textContent = "未知";
    $("run-graph").replaceChildren();
    $("run-nodes").replaceChildren();
    $("run-values").textContent = "";
    $("run-source").textContent = "";
    closeStream();
    state.activeRun = id;
    state.gap = false;
    state.runDefinition = null;
    $("timeline").replaceChildren();
    $("stream-state").textContent = "正在读取所选运行及其版本…";
    const [snapshot, definition] = await Promise.all([
      api(`runs/${id}`),
      api(`runs/${id}/definition`),
    ]);
    if (state.activeRun !== id) return;
    state.runDefinition = definition;
    mode("monitor");
    $("run-source").textContent = definition.source;
    facts(snapshot);
    history.replaceState(
      null,
      "",
      location.pathname + location.search + "#run=" + id,
    );
    if (snapshot.final || snapshot.owned === false) {
      $("stream-state").textContent = snapshot.final
        ? "已读取保存的终态；查看没有重新执行。"
        : "原观察者已结束，结果或清理仍未知；此服务没有接管。";
      await runHistory();
      return;
    }
    $("stream-state").textContent = "观察本次运行；关闭页面不会停止。";
    const stream = new EventSource(`/api/v1/runs/${id}/events`);
    state.stream = stream;
    stream.onopen = () => {
      if (state.activeRun === id) {
        $("connection-state").textContent = "已连接";
      }
    };
    stream.onmessage = (event) => {
      if (state.activeRun !== id) return;
      const data = JSON.parse(event.data);
      $("timeline").append(
        node(
          "li",
          `${data.seq} · ${data.kind} · ${data.node?.name || ""} ${data.node?.state || ""}`,
        ),
      );
      while ($("timeline").children.length > 100)
        $("timeline").firstChild.remove();
      if (!state.refreshTimer)
        state.refreshTimer = setTimeout(() => {
          state.refreshTimer = null;
          refreshRun(id).catch((e) => notice(e.message, true));
        }, 150);
    };
    stream.addEventListener("interrupted", (event) => {
      if (state.activeRun === id) {
        facts(JSON.parse(event.data));
        closeStream();
        $("stream-state").textContent =
          "原观察者已结束；此服务没有取得停止权限。";
      }
    });
    stream.addEventListener("snapshot", (event) => {
      if (state.activeRun === id) facts(JSON.parse(event.data));
    });
    stream.addEventListener("gap", () => {
      if (state.activeRun === id) {
        state.gap = true;
        $("stream-state").textContent = "事件存在缺口；快照只能恢复当前事实。";
      }
    });
    stream.addEventListener("final", (event) => {
      if (state.activeRun === id) {
        facts(JSON.parse(event.data));
        closeStream();
        $("stream-state").textContent = "终态已保存；快照不补造事件历史。";
        runHistory().catch((e) => notice(e.message, true));
      }
    });
    stream.onerror = () => {
      if (state.activeRun === id && state.stream) {
        state.gap = true;
        $("connection-state").textContent = "连接已断开";
        $("stream-state").textContent =
          "连接中断，等待重连与快照；运行仍由服务持有。";
      }
    };
  }
  async function start() {
    if (dirty() || !state.workflow) throw new Error("先保存版本再运行");
    const targets = Array.from($("run-targets").selectedOptions).map(
      (o) => o.value,
    );
    if (!targets.length) throw new Error("请选择目标节点");
    const timeout = Number($("timeout-ms").value);
    if (!Number.isSafeInteger(timeout) || timeout < 1 || timeout > 3600000)
      throw new Error("任务时限为 1..3600000 毫秒");
    const request = {
      workflow_id: state.workflow.workflow_id,
      revision: state.savedRevision,
      request_id: crypto.randomUUID(),
      target_nodes: targets,
      timeout_ms: timeout,
      preset_id: $("preset-select").value,
      retain_evidence: $("retain-evidence").checked,
    };
    const encoded = JSON.stringify(request),
      body = encoded.slice(0, -1) + ',"values":' + encodedValues() + "}";
    $("start-run").disabled = true;
    closeStream();
    state.activeRun = "";
    $("run-id").textContent = "";
    $("run-status").textContent = "pending";
    try {
      let submitted;
      try {
        submitted = await api("runs", { method: "POST", body });
      } catch (e) {
        if (e.status) throw e;
        try {
          submitted = await api(
            `run-requests/${encodeURIComponent(request.request_id)}`,
          );
        } catch {
          throw new Error(
            `提交结果未知，请按 request_id ${request.request_id} 查询；没有自动重跑。`,
          );
        }
      }
      closeDialog("launchpad");
      await openRun(submitted.run_id);
      await runHistory();
    } finally {
      syncButtons();
    }
  }
  async function runHistory(more = false) {
    if (!more) {
      state.runOffset = 0;
      $("run-history").replaceChildren();
    }
    const filter = state.workflow
      ? "&workflow_id=" + state.workflow.workflow_id
      : "";
    const data = await api(`runs?offset=${state.runOffset}&limit=20${filter}`);
    state.runOffset = data.next_offset;
    $("more-runs").hidden = !data.has_more;
    for (const item of data.runs) {
      const button = node(
        "button",
        `${item.run_id.slice(0, 8)} · ${item.task_result} · ${item.reason}`,
      );
      button.dataset.run = item.run_id;
      button.addEventListener(
        "click",
        guard(() => openRun(item.run_id)),
      );
      $("run-history").append(button);
    }
  }
  function busy(value) {
    editorBusy = value;
    if (value) {
      disabledBefore = Array.from(
        document.querySelectorAll(
          ".editor button,.editor input,.editor select,.editor textarea,aside button,aside input",
        ),
      ).map((e) => [e, e.disabled]);
      for (const [e] of disabledBefore) e.disabled = true;
    } else {
      for (const [e, disabled] of disabledBefore) e.disabled = disabled;
      disabledBefore = [];
      $("version-select").disabled = !state.workflow;
      $("apply-field").disabled =
        !state.document?.valid || state.document?.display.view.raw_edit_only;
      $("remove-field").disabled = $("apply-field").disabled;
      syncButtons();
    }
  }
  const guard =
    (fn, allowDuringBusy = false) =>
    async (event) => {
      event?.preventDefault();
      if (editorBusy && !allowDuringBusy) return;
      if (!allowDuringBusy) busy(true);
      try {
        await fn(event);
      } catch (e) {
        notice(e.message, true);
      } finally {
        if (!allowDuringBusy) busy(false);
      }
    };
  // Workbench state is separate from the immutable run definition.
  state.mode = "design";
  state.selected = "";
  state.direction = "";
  if (matchMedia("(max-width:700px)").matches)
    document.body.classList.add("sidebar-collapsed");
  const currentDisplay = () =>
    state.mode === "monitor"
      ? state.runDefinition?.display
      : state.document?.display;
  const currentCanvas = () =>
    $(state.mode === "monitor" ? "run-graph" : "editor-graph");
  const selectedNode = () =>
    currentDisplay()?.view.nodes.find((n) => n.id === state.selected);
  const conditionNames = {
    process_started: "启动后",
    process_completed: "结束后",
    process_completed_successfully: "成功完成后",
    process_healthy: "健康就绪后",
    process_log_ready: "日志就绪后",
  };
  function showDialog(id) {
    const dialog = $(id);
    if (!dialog.open) {
      dialog._returnFocus = document.activeElement;
      dialog.showModal();
    }
  }
  function closeDialog(id) {
    if ($(id).open) $(id).close();
  }
  for (const d of document.querySelectorAll("dialog")) {
    d.addEventListener("close", () => d._returnFocus?.focus());
    d.addEventListener("click", (e) => {
      if (e.target === d) {
        const b = d.getBoundingClientRect();
        if (
          e.clientX < b.left ||
          e.clientX > b.right ||
          e.clientY < b.top ||
          e.clientY > b.bottom
        )
          d.close();
      }
    });
  }
  document
    .querySelectorAll("[data-close]")
    .forEach((b) =>
      b.addEventListener("click", () => closeDialog(b.dataset.close)),
    );
  function mode(value) {
    state.mode = value;
    $("editor-graph").hidden = value !== "design";
    $("run-graph").hidden = value !== "monitor";
    $("design-mode").setAttribute("aria-pressed", String(value === "design"));
    $("monitor-mode").setAttribute("aria-pressed", String(value === "monitor"));
    $("canvas-mode").textContent =
      value === "design" ? "配置声明 · DESIGN" : "运行事实 · MONITOR";
    $("run-hud").hidden = value !== "monitor";
    $("add-node").hidden = value !== "design";
    $("inspector").hidden = true;
    state.direction = "";
    updateCanvas();
  }
  function updateCanvas() {
    const display = currentDisplay();
    $("canvas-summary").textContent = display
      ? `${display.view.nodes.length} 个节点 · ${display.view.edges?.length || 0} 条依赖`
      : "选择一次运行";
    RunCradleGraph.minimap($("minimap"), display, focusNode);
    if (state.selected)
      RunCradleGraph.spotlight(
        currentCanvas(),
        display,
        state.selected,
        state.direction,
      );
  }
  function focusNode(id) {
    selectWorkbenchNode(id);
    currentCanvas()
      .querySelector(`[data-node="${id}"]`)
      ?.scrollIntoView({ block: "center", inline: "center" });
  }
  function selectWorkbenchNode(id) {
    state.selected = id;
    const n = selectedNode();
    if (!n) return;
    $("inspector").hidden = false;
    $("inspector-title").textContent = n.label;
    $("inspector-id").textContent = n.name;
    $("config-tab").disabled = state.mode === "monitor";
    inspectorTab(state.mode === "monitor" ? "facts" : "config");
    if (state.mode === "design") {
      selectNode(id);
      fillInspector(n);
    }
    renderNodeFacts();
    updateCanvas();
  }
  function inspectorTab(tab) {
    $("node-config").hidden = tab !== "config";
    $("node-facts").hidden = tab !== "facts";
    $("config-tab").setAttribute("aria-pressed", String(tab === "config"));
    $("facts-tab").setAttribute("aria-pressed", String(tab === "facts"));
  }
  function renderNodeFacts() {
    const n = selectedNode(),
      fact = state.snapshot?.nodes?.find((f) => f.name === n?.name),
      el = $("node-facts");
    el.replaceChildren();
    if (!fact) {
      el.append(node("p", "尚无该节点的运行事实。"));
      return;
    }
    if (
      state.mode !== "monitor" &&
      state.document?.revision !== state.snapshot?.revision
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
          await wb({
            action: "unbind_input",
            process: n.name,
            input_name: e.bound_input,
            env_key: e.key,
          });
          return;
        }
        if (!key.value) throw new Error("先填写变量名");
        await applyNode();
        state.binding = { process: n.name, key: key.value };
        $("bind-name").value = key.value.toLowerCase();
        $("bind-type").value = "string";
        $("bind-default").value = value.value;
        $("bind-enum").value = "";
        showDialog("bind-dialog");
      }),
    );
    row.append(key, value, bind, removeButton(row));
    $("env-rows").append(row);
  }
  function dependencyRow(d = {}) {
    const row = node("div", undefined, "dep-row"),
      from = node("select"),
      condition = node("select");
    from.setAttribute("aria-label", "前置节点");
    condition.setAttribute("aria-label", "依赖条件");
    for (const n of state.document.display.view.nodes)
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
  function fillInspector(n) {
    $("node-name").value = n.name;
    $("node-label").value = n.label;
    $("command-mode").value =
      n.command_mode === "entrypoint" ? "entrypoint" : "command";
    $("node-command").value = n.declaration_text.command || "";
    $("node-working-dir").value = n.working_dir;
    $("node-restart").value = n.restart;
    $("node-exit-end").checked = n.exit_on_end;
    $("node-exit-skipped").checked = n.exit_on_skipped;
    $("entrypoint-rows").replaceChildren();
    n.entrypoint_items.forEach(argumentRow);
    $("env-rows").replaceChildren();
    n.env_items.forEach(envRow);
    $("dependency-rows").replaceChildren();
    n.dependencies.forEach(dependencyRow);
    const raw = state.document.display.view.raw_edit_only;
    $("raw-only").hidden = !raw;
    $("node-fields").disabled = raw || !state.document.valid;
    commandMode();
  }
  function commandMode() {
    const entry = $("command-mode").value === "entrypoint";
    $("entrypoint-editor").hidden = !entry;
    $("command-label").hidden = entry;
  }
  async function wb(edit, rawFields = {}) {
    const base = JSON.stringify({ ...draft(), workbench_edit: edit });
    let body = base;
    for (const [key, value] of Object.entries(rawFields)) {
      body = body.slice(0, -2) + "," + JSON.stringify(key) + ":" + value + "}}";
    }
    const data = await api("validate", { method: "POST", body });
    const selectedName =
      edit.action === "rename_process"
        ? edit.new_name
        : edit.process || selectedNode()?.name;
    renderDocument(data);
    mode("design");
    const n = data.display.view.nodes.find((n) => n.name === selectedName);
    if (n) selectWorkbenchNode(n.id);
    notice("已应用到草稿，保存后运行。");
    return data;
  }
  async function applyNode() {
    const n = selectedNode();
    if (!n) throw new Error("请选择节点");
    return wb({
      action: "update_process",
      process: n.name,
      command_mode: $("command-mode").value,
      command: $("node-command").value,
      entrypoint: [...$("entrypoint-rows").querySelectorAll("input")].map(
        (e) => e.value,
      ),
      working_dir: $("node-working-dir").value,
      env_items: [...$("env-rows").children].map((row) => ({
        key: row.querySelector(".env-key").value,
        value: row.querySelector(".env-value").value,
      })),
      depends_on: [...$("dependency-rows").children].map((row) => ({
        from: row.children[0].value,
        condition: row.children[1].value,
      })),
      restart: $("node-restart").value,
      exit_on_end: $("node-exit-end").checked,
      exit_on_skipped: $("node-exit-skipped").checked,
    });
  }
  function addNode(upstream = "") {
    state.addUpstream = upstream;
    $("add-upstream").textContent = upstream
      ? `前置 ${upstream} · 成功完成后启动`
      : "独立节点";
    $("add-name").value = "";
    showDialog("add-dialog");
  }
  function launch(target) {
    if (target) {
      for (const o of $("run-targets").options) o.selected = o.value === target;
    }
    $("launch-note").textContent = dirty()
      ? "请先保存草稿，再运行。"
      : "运行已保存的版本 " + state.savedRevision.slice(0, 12);
    showDialog("launchpad");
    syncButtons();
  }
  // All primitive values retain their original decimal text, including int64.
  function rawValues(text) {
    const values = {};
    const re =
      /"((?:[^"\\]|\\.)*)"\s*:\s*("(?:[^"\\]|\\.)*"|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null)/g;
    for (const m of text.matchAll(re))
      values[JSON.parse('"' + m[1] + '"')] = m[2];
    return values;
  }
  function typedRaw(type, value) {
    if (type === "string") return JSON.stringify(value);
    if (type === "boolean") {
      if (value !== "true" && value !== "false")
        throw new Error("boolean 值必须为 true 或 false");
      return value;
    }
    const raw = value.trim();
    if (
      !/^-?(0|[1-9]\d*)$/.test(raw) ||
      BigInt(raw) < -9223372036854775808n ||
      BigInt(raw) > 9223372036854775807n
    )
      throw new Error("integer 参数必须是精确的 int64 十进制整数");
    return raw;
  }
  function renderParameters(display) {
    $("parameter-inputs").replaceChildren();
    $("preset-select").replaceChildren(new Option("默认值", ""));
    $("preset-cards").replaceChildren();
    for (const p of display.metadata.presets || []) {
      $("preset-select").append(new Option(p.label || p.id, p.id));
      const b = node("button", p.label || p.id);
      b.addEventListener("click", () => {
        $("preset-select").value = p.id;
        presetDescription();
      });
      $("preset-cards").append(b);
    }
    for (const input of display.metadata.inputs || []) {
      const row = node("div", undefined, "parameter"),
        label = node("label", `${input.label || input.name} · ${input.type}`),
        mark = node("span", "", "modified");
      label.append(mark);
      let control;
      if (input.enum_text?.length) {
        control = node("select");
        control.dataset.enum = "true";
        for (const raw of input.enum_text)
          control.append(
            new Option(input.type === "string" ? JSON.parse(raw) : raw, raw),
          );
      } else if (input.type === "boolean") {
        control = node("select");
        control.append(
          new Option("true", "true"),
          new Option("false", "false"),
        );
      } else {
        control = node("input");
        if (input.type === "integer") control.inputMode = "numeric";
      }
      control.dataset.input = input.name;
      control.dataset.kind = input.type;
      control.setAttribute("aria-label", input.label || input.name);
      control.addEventListener("input", () => {
        control.dataset.modified = "true";
        mark.textContent =
          control.value !== control.dataset.baseline ? "Modified" : "";
      });
      row.append(label, control);
      $("parameter-inputs").append(row);
    }
    presetDescription();
  }
  function presetDescription() {
    const display = state.document?.display;
    if (!display) return;
    const preset = display.metadata.presets?.find(
        (p) => p.id === $("preset-select").value,
      ),
      values = rawValues(preset?.values_text || "{}");
    $("preset-values").textContent = preset?.values_text || "默认值";
    for (const inpt of display.metadata.inputs || []) {
      const control = [
          ...$("parameter-inputs").querySelectorAll("[data-input]"),
        ].find((c) => c.dataset.input === inpt.name),
        raw = values[inpt.name] ?? inpt.default_text;
      control.dataset.hasBase = String(raw !== undefined);
      control.value =
        raw === undefined
          ? ""
          : control.dataset.enum === "true" || inpt.type !== "string"
            ? raw
            : JSON.parse(raw);
      control.dataset.baseline = control.value;
      control.dataset.modified = "false";
      control.parentElement.querySelector(".modified").textContent = "";
    }
  }
  function encodedValues() {
    const pairs = [];
    for (const c of $("parameter-inputs").querySelectorAll("[data-input]")) {
      if (c.dataset.hasBase !== "true" && c.dataset.modified !== "true")
        continue;
      const raw =
        c.dataset.enum === "true" ? c.value : typedRaw(c.dataset.kind, c.value);
      pairs.push(JSON.stringify(c.dataset.input) + ":" + raw);
    }
    return "{" + pairs.join(",") + "}";
  }
  function showDiff(title, left, right) {
    const diff = RunCradleGraph.lineDiff(left, right);
    $("diff-title").textContent =
      title + (diff.limited ? " · 大文件：显示变更区块" : "");
    $("diff-columns").replaceChildren();
    for (const [name, lines, kind] of [
      ["当前草稿 / 已保存版本", diff.left, "removed"],
      ["最新版本 / 导入文件", diff.right, "added"],
    ]) {
      const pane = node("section", undefined, "diff-pane");
      pane.append(node("h3", name));
      lines.forEach((line, i) =>
        pane.append(
          node(
            "span",
            `${i + 1}  ${line.text}`,
            "diff-line " + (line.changed ? kind : ""),
          ),
        ),
      );
      $("diff-columns").append(pane);
    }
    showDialog("diff-dialog");
  }
  function sourceLines() {
    for (const p of ["native", "ui"]) {
      const area = $(p + "-source"),
        lines = $(p + "-lines");
      lines.textContent = Array.from(
        { length: area.value.split("\n").length },
        (_, i) => i + 1,
      ).join("\n");
      lines.scrollTop = area.scrollTop;
    }
  }
  async function importPicker() {
    showDialog("file-picker");
    const result = await api("workspace-files");
    state.files = result.files;
    $("file-budget").textContent = result.truncated
      ? "列表已截断，可手动输入工作区相对路径。"
      : `${result.files.length} 个候选文件`;
    $("ui-file-options").replaceChildren(
      ...result.ui_files.map((p) => new Option(p, p)),
    );
    fileResults();
  }
  function fileResults() {
    const q = $("file-search").value.toLowerCase();
    $("file-results").replaceChildren();
    for (const path of (state.files || []).filter((p) =>
      p.toLowerCase().includes(q),
    )) {
      const b = node("button", path);
      b.addEventListener("click", () => {
        $("import-file-path").value = path;
        const dir = path.includes("/")
          ? path.slice(0, path.lastIndexOf("/") + 1)
          : "";
        if (
          [...$("ui-file-options").options].some(
            (o) => o.value === dir + "ui.json",
          )
        )
          $("import-ui-path").value = dir + "ui.json";
      });
      $("file-results").append(b);
    }
  }
  function search() {
    const q = $("global-search").value.toLowerCase();
    $("search-results").replaceChildren();
    for (const n of currentDisplay()?.view.nodes || []) {
      if (
        ![
          n.name,
          n.label,
          n.command_preview,
          ...n.env_items.map((e) => e.key + "=" + e.value),
        ]
          .join(" ")
          .toLowerCase()
          .includes(q)
      )
        continue;
      const b = node("button", `${n.label} · ${n.command_preview}`);
      b.addEventListener("click", () => {
        closeDialog("search-dialog");
        focusNode(n.id);
      });
      $("search-results").append(b);
    }
    for (const b of $("workflow-list").querySelectorAll("button")) {
      if (b.textContent.toLowerCase().includes(q)) {
        const c = node("button", "工作流 · " + b.textContent);
        c.addEventListener("click", () => {
          closeDialog("search-dialog");
          b.click();
        });
        $("search-results").append(c);
      }
    }
    for (const [label, id] of [
      ["保存版本", "save-workflow"],
      ["运行", "open-launchpad"],
      ["源码", "open-source"],
      ["导入", "open-import"],
    ]) {
      if (!label.includes(q) && q) continue;
      const b = node("button", "操作 · " + label);
      b.addEventListener("click", () => {
        closeDialog("search-dialog");
        $(id).click();
      });
      $("search-results").append(b);
    }
  }
  function on(id, fn, guarded = false) {
    $(id).addEventListener("click", guarded ? guard(fn) : fn);
  }
  on("toggle-sidebar", () =>
    document.body.classList.toggle("sidebar-collapsed"),
  );
  on("design-mode", () => mode("design"));
  on("monitor-mode", () => mode("monitor"));
  on("close-inspector", () => {
    $("inspector").hidden = true;
  });
  on("config-tab", () => inspectorTab("config"));
  on("facts-tab", () => {
    inspectorTab("facts");
    renderNodeFacts();
  });
  on("open-source", () => {
    sourceLines();
    showDialog("source-dialog");
  });
  on(
    "source-apply",
    async () => {
      const d = await validate();
      sourceLines();
      if (d.valid) closeDialog("source-dialog");
    },
    true,
  );
  on("open-launchpad", () => launch());
  on("node-run", () => launch(selectedNode()?.name));
  on("add-node", () => addNode());
  on(
    "confirm-add",
    async () => {
      const name = $("add-name").value;
      await wb({
        action: "add_process",
        process: name,
        upstream: state.addUpstream,
      });
      closeDialog("add-dialog");
    },
    true,
  );
  on("apply-node", applyNode, true);
  on(
    "rename-node",
    async () => {
      const n = selectedNode(),
        name = $("node-name").value;
      await wb({
        action: name === n.name ? "update_process" : "rename_process",
        process: n.name,
        new_name: name,
        label: $("node-label").value,
      });
    },
    true,
  );
  on(
    "delete-node",
    async () => {
      await wb({ action: "delete_process", process: selectedNode().name });
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
          bindings: [{ ...state.binding, field: "environment" }],
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
      await wb({ action: "bind_input" }, { input_spec: raw });
      closeDialog("bind-dialog");
    },
    true,
  );
  on(
    "save-preset",
    async () => {
      const id = $("preset-name").value.trim();
      if (!id) throw new Error("填写预设名称");
      const raw =
        JSON.stringify({ id, label: id }).slice(0, -1) +
        ',"values":' +
        encodedValues() +
        "}";
      await wb({ action: "save_preset" }, { preset_spec: raw });
      closeDialog("launchpad");
    },
    true,
  );
  on(
    "delete-preset",
    async () => {
      await wb({
        action: "delete_preset",
        preset_id: $("preset-select").value,
      });
      closeDialog("launchpad");
    },
    true,
  );
  on("open-import", importPicker, true);
  $("file-search").addEventListener("input", fileResults);
  on("open-search", () => {
    showDialog("search-dialog");
    search();
    $("global-search").focus();
  });
  $("global-search").addEventListener("input", search);
  on("toggle-console", () => {
    $("console-body").hidden = !$("console-body").hidden;
  });
  $("event-filter").addEventListener("input", () => {
    for (const e of $("timeline").children)
      e.hidden = !e.textContent.includes($("event-filter").value);
  });
  for (const [id, panel] of [
    ["library-tab", "library"],
    ["history-tab", "history"],
  ])
    on(id, () => {
      $("library-panel").hidden = panel !== "library";
      $("history-panel").hidden = panel !== "history";
      $("library-tab").setAttribute(
        "aria-pressed",
        String(panel === "library"),
      );
      $("history-tab").setAttribute(
        "aria-pressed",
        String(panel === "history"),
      );
    });
  $("library-filter").addEventListener("input", () => {
    for (const e of document.querySelectorAll(
      "#workflow-list button,#run-history button",
    ))
      e.hidden = !e.textContent
        .toLowerCase()
        .includes($("library-filter").value.toLowerCase());
  });
  on("zoom-in", () =>
    RunCradleGraph.setZoom(
      currentCanvas(),
      (Number(currentCanvas().dataset.zoom) || 1) * 1.2,
    ),
  );
  on("zoom-out", () =>
    RunCradleGraph.setZoom(
      currentCanvas(),
      (Number(currentCanvas().dataset.zoom) || 1) / 1.2,
    ),
  );
  on("fit", () => {
    const d = currentDisplay();
    if (d)
      RunCradleGraph.setZoom(
        currentCanvas(),
        (currentCanvas().clientWidth - 60) / d.graph.width,
      );
  });
  on("map-toggle", () => {
    $("minimap").hidden = !$("minimap").hidden;
    updateCanvas();
  });
  for (const [id, dir] of [
    ["trace-up", "upstream"],
    ["trace-down", "downstream"],
    ["trace-clear", ""],
  ])
    on(id, () => {
      state.direction = dir;
      updateCanvas();
    });
  on("theme-toggle", () => document.body.classList.toggle("light"));
  $("visual-style").addEventListener("change", () =>
    document.body.classList.toggle(
      "blueprint",
      $("visual-style").value === "blueprint",
    ),
  );
  for (const p of ["native", "ui"]) {
    $(p + "-source").addEventListener("input", sourceLines);
    $(p + "-source").addEventListener("scroll", sourceLines);
  }
  document.addEventListener("keydown", (e) => {
    const editing =
      e.target.matches("input,textarea,select") || e.target.isContentEditable;
    if (
      ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") ||
      (e.key === "/" && !editing)
    ) {
      e.preventDefault();
      $("open-search").click();
      return;
    }
    if (editing || document.querySelector("dialog[open]")) return;
    const keys = {
      "+": "zoom-in",
      "=": "zoom-in",
      "-": "zoom-out",
      0: "fit",
      m: "map-toggle",
      t: "theme-toggle",
      "[": "toggle-sidebar",
    };
    if (keys[e.key]) {
      e.preventDefault();
      $(keys[e.key]).click();
    }
  });
  $("drop-zone").addEventListener("dragover", (e) => e.preventDefault());
  $("drop-zone").addEventListener(
    "drop",
    guard(async (e) => {
      e.preventDefault();
      const file = e.dataTransfer.files[0];
      if (!file) return;
      const source = new TextDecoder("utf-8", { fatal: true }).decode(
        await file.arrayBuffer(),
      );
      const data = await api("validate", {
        method: "POST",
        body: {
          format: file.name.endsWith(".json") ? "json" : "yaml",
          source,
          entry: file.name,
        },
      });
      renderDocument(data, { fresh: true });
      closeDialog("file-picker");
    }),
  );
  const elapsedTimer = setInterval(() => {
    if (state.mode !== "monitor") return;
    for (const el of $("run-graph").querySelectorAll("[data-start]")) {
      if (!el.dataset.start) continue;
      const start = Date.parse(el.dataset.start),
        end = el.dataset.end ? Date.parse(el.dataset.end) : Date.now();
      if (Number.isFinite(start) && Number.isFinite(end))
        el.textContent = ((end - start) / 1000).toFixed(1) + "s";
    }
  }, 1000);
  window.addEventListener("pagehide", () => clearInterval(elapsedTimer));
  $("new-workflow").addEventListener("click", guard(fresh));
  $("validate-draft").addEventListener(
    "click",
    guard(async () => {
      const d = await validate();
      if (d.issues?.length) showDialog("source-dialog");
    }),
  );
  $("save-workflow").addEventListener("click", guard(save));
  $("apply-field").addEventListener(
    "click",
    guard(() => edit()),
  );
  $("remove-field").addEventListener(
    "click",
    guard(() => edit(true)),
  );
  $("edit-node").addEventListener("change", () => {
    const n = state.document.display.view.nodes.find(
      (n) => n.name === $("edit-node").value,
    );
    if (n) selectNode(n.id);
  });
  $("edit-field").addEventListener("change", fieldValue);
  $("preset-select").addEventListener("change", presetDescription);
  for (const id of ["native-source", "ui-source", "source-format"])
    $(id).addEventListener("input", markDirty);
  $("import-workspace").addEventListener(
    "click",
    guard(async () => {
      const data = await api("imports", {
        method: "POST",
        body: {
          file: $("import-file-path").value,
          ui: $("import-ui-path").value,
        },
      });
      renderDocument(data, { fresh: true });
      closeDialog("file-picker");
      if (matchMedia("(max-width:700px)").matches)
        document.body.classList.add("sidebar-collapsed");
      notice("已读取预览；尚未保存或执行。");
    }),
  );
  $("reimport-workspace").addEventListener(
    "click",
    guard(async () => {
      const data = await api("imports", {
        method: "POST",
        body: {
          file: $("import-file-path").value || state.document.entry,
          ui: $("import-ui-path").value || state.document.ui_path || "",
        },
      });
      const saved = await api(
        `workflows/${state.workflow.workflow_id}?revision=${state.savedRevision}`,
      );
      state.importPreview = data;
      $("reimport-old").textContent =
        `${saved.revision}\n${saved.source}\n${saved.ui_source || ""}`;
      $("reimport-new").textContent =
        `${data.revision}\n${data.source}\n${data.ui_source || ""}`;
      $("reimport-preview").hidden = false;
      showDiff(
        "重新导入对比",
        saved.source + "\n" + (saved.ui_source || ""),
        data.source + "\n" + (data.ui_source || ""),
      );
    }),
  );
  $("apply-reimport").addEventListener(
    "click",
    guard(() => {
      if (!state.importPreview) return;
      renderDocument(state.importPreview);
      closeDialog("diff-dialog");
      notice(
        "重新导入内容已应用到草稿；保存仍比较当前库版本，源文件保持原样。",
      );
    }),
  );
  $("dismiss-reimport").addEventListener("click", () => {
    $("reimport-preview").hidden = true;
    state.importPreview = null;
    closeDialog("diff-dialog");
  });
  async function upload() {
    const file = $("upload-native").files[0];
    if (!file) return;
    const decode = async (f) =>
      new TextDecoder("utf-8", { fatal: true }).decode(await f.arrayBuffer());
    const input = {
      format: file.name.toLowerCase().endsWith(".json") ? "json" : "yaml",
      source: await decode(file),
      entry: file.name,
    };
    const ui = $("upload-ui").files[0];
    if (ui) {
      input.ui_source = await decode(ui);
      input.ui_path = ui.name;
    }
    const data = await api("validate", { method: "POST", body: input });
    renderDocument(data, { fresh: true });
  }
  $("upload-native").addEventListener("change", guard(upload));
  $("upload-ui").addEventListener("change", guard(upload));
  $("more-workflows").addEventListener(
    "click",
    guard(() => library(true)),
  );
  $("more-runs").addEventListener(
    "click",
    guard(() => runHistory(true)),
  );
  $("start-run").addEventListener("click", guard(start));
  $("stop-run").addEventListener(
    "click",
    guard(async () => {
      const id = state.renderedRun;
      if (!id || id !== state.activeRun || $("stop-run").disabled) return;
      await api(`runs/${id}/stop`, { method: "POST", body: {} });
      notice("停止请求已受理；终态和清理仍待观察。");
      if (state.activeRun === id) await refreshRun(id);
    }, true),
  );
  $("reload-latest").addEventListener(
    "click",
    guard(async () => {
      await openWorkflow(state.workflow.workflow_id);
      closeDialog("diff-dialog");
    }),
  );
  $("version-select").addEventListener(
    "change",
    guard(() =>
      openWorkflow(state.workflow.workflow_id, $("version-select").value),
    ),
  );
  $("copy-workflow").addEventListener(
    "click",
    guard(async () => {
      const copy = await api(`workflows/${state.workflow.workflow_id}/copy`, {
        method: "POST",
        body: { revision: state.savedRevision },
      });
      renderDocument(copy, { saved: true, fresh: true });
      await library();
      await runHistory();
      notice("已复制，使用新的工作流身份。");
    }),
  );
  $("export-workflow").addEventListener("click", () => {
    if (!state.workflow) return;
    const link = node("a");
    link.href = `/api/v1/workflows/${state.workflow.workflow_id}/export?revision=${state.savedRevision}`;
    link.download = "runcradle-config.zip";
    link.click();
  });
  window.addEventListener("pagehide", closeStream);
  busy(true);
  (async () => {
    const fragment = new URLSearchParams(location.hash.slice(1));
    const pair = fragment.get("pair"),
      run = fragment.get("run");
    if (pair) {
      history.replaceState(null, "", location.pathname + location.search);
      await api("session", { method: "POST", body: { token: pair } });
    }
    await library();
    const workflowID = new URLSearchParams(location.search).get("workflow");
    if (workflowID) await openWorkflow(workflowID);
    else await fresh();
    await runHistory();
    if (run && /^[a-f0-9]{32}$/.test(run)) await openRun(run);
    const caps = await api("capabilities");
    $("engine-state").textContent = caps.engine_available
      ? `${caps.engine.engine_kind} ${caps.engine.engine_version}`
      : "引擎未就绪 · 可编辑保存";
  })()
    .catch((e) => {
      notice(
        `无法连接工作区：${e.message}。首次打开请使用 serve 提供的一次性配对链接。`,
        true,
      );
      $("connection-state").textContent = "未连接";
    })
    .finally(() => busy(false));
})();
