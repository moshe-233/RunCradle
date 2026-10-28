import { $, node } from "./view.js";

// 编辑与版本只修改 editorState；运行定义通过只读视图取得。
export function createEditor({
  editorState,
  api,
  clearSelection,
  closeDialog,
  drawGraph,
  guard,
  setMode,
  notice,
  on,
  readInspector,
  readRuns,
  readView,
  rememberInspector,
  resetInspectorDrafts,
  runHistory,
  selectWorkbenchNode,
  selectedNode,
  showDialog,
  showDiff,
  sourceLines,
  updateCanvas,
}) {
  const normalize = (text) => text.replace(/\r\n?/g, "\n");

  function draft() {
  // 文本框会规范化换行；未修改时保留原字节，避免无意改变 revision。
    const old = editorState.document;
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
    return !sameDraft(draft(), editorState.committed);
  }

  function syncButtons() {
    $("draft-status").textContent = dirty()
      ? "草稿已修改 · 未保存"
      : "已保存 · 运行固定此版本";
    $("start-run").disabled =
      readView().editorBusy ||
      !editorState.workflow ||
      dirty() ||
      !editorState.document?.valid ||
      !editorState.document?.run_supported;
    $("copy-workflow").disabled = !editorState.workflow;
    $("export-workflow").disabled = !editorState.workflow;
    $("reimport-workspace").disabled = !editorState.workflow;
  }

  function markDirty() {
    syncButtons();
    $("execution-note").textContent =
      "草稿修改后需要校验并保存；当前运行仍使用原版本。";
  }

  function fieldValue() {
    const selected = editorState.document?.display?.view.nodes.find(
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
    const selected = editorState.document?.display?.view.nodes.find(
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
    rememberInspector();
    if (fresh) {
      setMode("design");
      resetInspectorDrafts();
    }
    $("reimport-preview").hidden = true;
    editorState.importPreview = null;
    if (fresh) {
      editorState.workflow = null;
      editorState.expected = "";
      editorState.savedRevision = "";
      editorState.committed = null;
      $("conflict").hidden = true;
    }
    editorState.document = data;
    $("source-format").value = data.format;
    $("native-source").value = data.source;
    $("ui-source").value = data.ui_source || "";
    $("workflow-title").textContent = data.display.view.title || "工作流";
    if (saved) {
      editorState.workflow = data.workflow;
      editorState.expected = data.workflow.revision;
      editorState.savedRevision = data.revision;
      editorState.committed = {
        format: data.format,
        source: data.source,
        entry: data.entry,
        ui_source: data.ui_source,
        ui_path: data.ui_path,
      };
      $("conflict").hidden = true;
    }
    $("workflow-id").textContent = editorState.workflow?.workflow_id || "";
    $("saved-revision").textContent = editorState.savedRevision;
    $("version-select").replaceChildren();
    $("version-select").disabled = !editorState.workflow;
    if (editorState.workflow) {
      for (const revision of [...editorState.workflow.revisions].reverse())
        $("version-select").append(new Option(revision.slice(0, 12), revision));
      $("version-select").value = editorState.savedRevision;
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
    drawGraph($("editor-graph"), data.display);
    renderParameters(data.display);
    syncButtons();
    sourceLines();
    updateCanvas();
    if (readView().mode === "design" && !$("inspector").hidden) {
      const selected = data.display.view.nodes.find(
        (n) => n.id === readInspector().selected,
      );
      if (selected) selectWorkbenchNode(selected.id, false);
      else {
        $("inspector").hidden = true;
        clearSelection();
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
      editorState.workflowOffset = 0;
      $("workflow-list").replaceChildren();
    }
    const data = await api(`workflows?offset=${editorState.workflowOffset}&limit=20`);
    editorState.workflowOffset = data.next_offset;
    $("more-workflows").hidden = !data.has_more;
    for (const item of data.workflows) {
      const button = node(
        "button",
        `${item.title} · ${item.revision.slice(0, 8)}`,
      );
      button.dataset.workflow = item.workflow_id;
      button.setAttribute(
        "aria-current",
        String(item.workflow_id === editorState.workflow?.workflow_id),
      );
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
      const id = editorState.workflow?.workflow_id;
      const saved = await api(id ? `workflows/${id}` : "workflows", {
        method: id ? "PUT" : "POST",
        body: {
          ...draft(),
          ...(id ? { expected_revision: editorState.expected } : {}),
        },
      });
      renderDocument(saved, { saved: true });
      history.replaceState(
        null,
        "",
        `/?workflow=${saved.workflow.workflow_id}` +
          (readRuns().activeRun ? "#run=" + readRuns().activeRun : ""),
      );
      notice("版本已保存，导入源文件未修改。");
      await library();
      await runHistory();
    } catch (e) {
      if (e.status === 409 && editorState.workflow) {
        const latest = await api(`workflows/${editorState.workflow.workflow_id}`);
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

  async function applyWorkbenchEdit(edit, rawFields = {}, source = draft()) {
    const base = JSON.stringify({ ...source, workbench_edit: edit });
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
    setMode("design");
    const n = data.display.view.nodes.find((n) => n.name === selectedName);
    if (n) selectWorkbenchNode(n.id);
    notice("已应用到草稿，保存后运行。");
    return data;
  }

  function rawValues(text) {
  // 保留十进制原文，不能先转 Number，否则 int64 会丢精度。
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
    const display = editorState.document?.display;
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

  async function importPicker() {
    showDialog("file-picker");
    const result = await api("workspace-files");
    editorState.files = result.files;
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
    for (const path of (editorState.files || []).filter((p) =>
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

  function readEditor() {
    return Object.freeze({ ...editorState });
  }
  let bound = false;
  function bindEvents() {
    if (bound) return;
    bound = true;
    on(
      "source-apply",
      async () => {
        const d = await validate();
        sourceLines();
        if (d.valid) closeDialog("source-dialog");
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
        await applyWorkbenchEdit({ action: "save_preset" }, { preset_spec: raw });
        closeDialog("launchpad");
      },
      true,
    );
    on(
      "delete-preset",
      async () => {
        await applyWorkbenchEdit({
          action: "delete_preset",
          preset_id: $("preset-select").value,
        });
        closeDialog("launchpad");
      },
      true,
    );
    on("open-import", importPicker, true);
    $("file-search").addEventListener("input", fileResults);
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
      const n = editorState.document.display.view.nodes.find(
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
            file: $("import-file-path").value || editorState.document.entry,
            ui: $("import-ui-path").value || editorState.document.ui_path || "",
          },
        });
        const saved = await api(
          `workflows/${editorState.workflow.workflow_id}?revision=${editorState.savedRevision}`,
        );
        editorState.importPreview = data;
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
        if (!editorState.importPreview) return;
        renderDocument(editorState.importPreview);
        closeDialog("diff-dialog");
        notice(
          "重新导入内容已应用到草稿；保存仍比较当前库版本，源文件保持原样。",
        );
      }),
    );
    $("dismiss-reimport").addEventListener("click", () => {
      $("reimport-preview").hidden = true;
      editorState.importPreview = null;
      closeDialog("diff-dialog");
    });
    $("upload-native").addEventListener("change", guard(upload));
    $("upload-ui").addEventListener("change", guard(upload));
    $("more-workflows").addEventListener(
      "click",
      guard(() => library(true)),
    );
    $("reload-latest").addEventListener(
      "click",
      guard(async () => {
        await openWorkflow(editorState.workflow.workflow_id);
        closeDialog("diff-dialog");
      }),
    );
    $("version-select").addEventListener(
      "change",
      guard(() =>
        openWorkflow(editorState.workflow.workflow_id, $("version-select").value),
      ),
    );
    $("copy-workflow").addEventListener(
      "click",
      guard(async () => {
        const copy = await api(`workflows/${editorState.workflow.workflow_id}/copy`, {
          method: "POST",
          body: { revision: editorState.savedRevision },
        });
        renderDocument(copy, { saved: true, fresh: true });
        await library();
        await runHistory();
        notice("已复制，使用新的工作流身份。");
      }),
    );
    $("export-workflow").addEventListener("click", () => {
      if (!editorState.workflow) return;
      const link = node("a");
      link.href = `/api/v1/workflows/${editorState.workflow.workflow_id}/export?revision=${editorState.savedRevision}`;
      link.download = "runcradle-config.zip";
      link.click();
    });
  }
  return {
    dirty,
    encodedValues,
    syncButtons,
    draft,
    selectNode,
    typedRaw,
    applyWorkbenchEdit,
    fresh,
    library,
    openWorkflow,
    read: readEditor,
    bindEvents,
  };
}
