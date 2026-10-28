export const $ = (id) => document.getElementById(id);
export const node = (tag, text, cls) => {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  if (cls) e.className = cls;
  return e;
};

// 页面交互持有模式、焦点与忙碌状态；业务操作由入口注入。
export function createView({
  viewState,
  addNode,
  closeInspector,
  inspectorOverlay,
  inspectorPresentation,
  launch,
  readEditor,
  readInspector,
  readRuns,
  rememberInspector,
  selectWorkbenchNode,
  syncButtons,
}) {
  function notice(message, error = false) {
    $("notice").hidden = false;
    $("notice").textContent = message;
    $("notice").classList.toggle("error", error);
  }

  function drawGraph(container, display, facts = []) {
    RunCradleGraph.draw(
      container,
      display,
      selectWorkbenchNode,
      facts,
      container.id === "editor-graph" ? { run: launch, add: addNode } : {},
    );
    container._display = display;
    requestAnimationFrame(() => autoFit(container));
    updateCanvas();
  }

  function autoFit(container) {
    const display = container._display;
    if (
      container.dataset.autoFit !== "false" &&
      display?.graph &&
      container.clientWidth
    )
      RunCradleGraph.setZoom(
        container,
        Math.min(1, (container.clientWidth - 40) / display.graph.width),
      );
  }

  function setEditorBusy(value) {
    viewState.editorBusy = value;
    if (value) {
      viewState.disabledBefore = Array.from(
        document.querySelectorAll(
          ".editor button,.editor input,.editor select,.editor textarea,aside button,aside input",
        ),
      ).map((e) => [e, e.disabled]);
      for (const [e] of viewState.disabledBefore) e.disabled = true;
    } else {
      for (const [e, disabled] of viewState.disabledBefore) e.disabled = disabled;
      viewState.disabledBefore = [];
      $("version-select").disabled = !readEditor().workflow;
      $("apply-field").disabled =
        !readEditor().document?.valid || readEditor().document?.display.view.raw_edit_only;
      $("remove-field").disabled = $("apply-field").disabled;
      syncButtons();
    }
  }

  const guard =
    (fn, allowDuringBusy = false) =>
    async (event) => {
      event?.preventDefault();
      if (viewState.editorBusy && !allowDuringBusy) return;
      if (!allowDuringBusy) setEditorBusy(true);
      try {
        await fn(event);
      } catch (e) {
        notice(e.message, true);
      } finally {
        if (!allowDuringBusy) setEditorBusy(false);
      }
    };

  const currentDisplay = () =>
    viewState.mode === "monitor"
      ? readRuns().runDefinition?.display
      : readEditor().document?.display;

  const currentCanvas = () =>
    $(viewState.mode === "monitor" ? "run-graph" : "editor-graph");

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

  function setMode(value) {
    rememberInspector();
    viewState.mode = value;
    $("editor-graph").hidden = value !== "design";
    $("run-graph").hidden = value !== "monitor";
    $("design-mode").setAttribute("aria-pressed", String(value === "design"));
    $("monitor-mode").setAttribute("aria-pressed", String(value === "monitor"));
    $("canvas-mode").textContent = value === "design" ? "配置声明" : "运行事实";
    $("run-hud").hidden = value !== "monitor";
    $("add-node").hidden = value !== "design";
    $("inspector").hidden = true;
    viewState.direction = "";
    updateCanvas();
  }

  function updateCanvas() {
    const display = currentDisplay();
    $("canvas-summary").textContent = display
      ? `${display.view.nodes.length} 个节点 · ${display.view.edges?.length || 0} 条依赖`
      : "选择一次运行";
    RunCradleGraph.minimap($("minimap"), display, focusNode);
    if (readInspector().selected)
      RunCradleGraph.spotlight(
        currentCanvas(),
        display,
        readInspector().selected,
        viewState.direction,
      );
  }

  function focusNode(id) {
    selectWorkbenchNode(id);
    currentCanvas()
      .querySelector(`[data-node="${id}"]`)
      ?.scrollIntoView({ block: "center", inline: "center" });
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

  function readView() {
    return Object.freeze({ ...viewState });
  }
  let bound = false;
  function bindEvents() {
    if (bound) return;
    bound = true;
    if (matchMedia("(max-width:700px)").matches)
      document.body.classList.add("sidebar-collapsed");
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
    on("toggle-sidebar", () =>
      document.body.classList.toggle("sidebar-collapsed"),
    );
    on("design-mode", () => setMode("design"));
    on("monitor-mode", () => setMode("monitor"));
    on("open-source", () => {
      sourceLines();
      showDialog("source-dialog");
    });
    on("open-search", () => {
      showDialog("search-dialog");
      search();
      $("global-search").focus();
    });
    $("global-search").addEventListener("input", search);
    on("toggle-console", () => {
      $("console-body").hidden = !$("console-body").hidden;
      $("toggle-console").setAttribute(
        "aria-expanded",
        String(!$("console-body").hidden),
      );
    });
    for (const tab of ["overview", "logs", "details"]) {
      const button = $("evidence-" + tab + "-tab");
      on(button.id, () => {
        for (const name of ["overview", "logs", "details"]) {
          $("evidence-" + name).hidden = name !== tab;
          $("evidence-" + name + "-tab").setAttribute(
            "aria-selected",
            String(name === tab),
          );
        }
      });
    }
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
    on(
      "zoom-in",
      () => (
        (currentCanvas().dataset.autoFit = "false"),
        RunCradleGraph.setZoom(
          currentCanvas(),
          (Number(currentCanvas().dataset.zoom) || 1) * 1.2,
        )
      ),
    );
    on(
      "zoom-out",
      () => (
        (currentCanvas().dataset.autoFit = "false"),
        RunCradleGraph.setZoom(
          currentCanvas(),
          (Number(currentCanvas().dataset.zoom) || 1) / 1.2,
        )
      ),
    );
    on("fit", () => {
      currentCanvas().dataset.autoFit = "true";
      const d = currentDisplay();
      if (d)
        RunCradleGraph.setZoom(
          currentCanvas(),
          Math.min(1, (currentCanvas().clientWidth - 40) / d.graph.width),
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
        viewState.direction = dir;
        updateCanvas();
      });
    RunCradleTheme.mount("theme-toggle", "visual-style");
    const canvasResize = new ResizeObserver((entries) =>
      entries.forEach((entry) => autoFit(entry.target)),
    );
    canvasResize.observe($("editor-graph"));
    canvasResize.observe($("run-graph"));
    matchMedia("(max-width: 1250px)").addEventListener("change", () => {
      inspectorPresentation(!$("inspector").hidden);
    });
    matchMedia("(max-width: 700px)").addEventListener("change", (event) => {
      if (event.matches) document.body.classList.add("sidebar-collapsed");
    });
    for (const p of ["native", "ui"]) {
      $(p + "-source").addEventListener("input", sourceLines);
      $(p + "-source").addEventListener("scroll", sourceLines);
    }
    document.addEventListener("keydown", (e) => {
      if (
        !$("inspector").hidden &&
        inspectorOverlay() &&
        !document.querySelector("dialog[open]")
      ) {
        if (e.key === "Escape") {
          e.preventDefault();
          closeInspector();
          return;
        }
        if (e.key === "Tab") {
          const items = [
            ...$("inspector").querySelectorAll(
              "button,input,textarea,select,summary,[tabindex]",
            ),
          ].filter(
            (el) =>
              !el.matches(":disabled") &&
              el.tabIndex >= 0 &&
              el.getClientRects().length,
          );
          const index = items.indexOf(document.activeElement);
          if (
            index < 0 ||
            (!e.shiftKey && index === items.length - 1) ||
            (e.shiftKey && index === 0)
          ) {
            e.preventDefault();
            (e.shiftKey ? items.at(-1) : items[0])?.focus();
          }
        }
      }
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
    window.addEventListener("pagehide", () => canvasResize.disconnect());
  }
  return {
    closeDialog,
    drawGraph,
    guard,
    setMode,
    notice,
    on,
    showDialog,
    showDiff,
    sourceLines,
    updateCanvas,
    focusNode,
    currentCanvas,
    currentDisplay,
    setEditorBusy,
    read: readView,
    bindEvents,
  };
}
