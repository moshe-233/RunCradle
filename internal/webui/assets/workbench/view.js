import { bindListFeedback, bindHelp } from "./feedback.js";
export const $ = (id) => document.getElementById(id);
export const node = (tag, text, cls) => {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  if (cls) e.className = cls;
  return e;
};

// Motion is decorative: no business or focus operation waits for completion.
const activeMotion = new Set();
const loading = new Map();
export function animateOnce(element, frames, duration = 180) {
  if (!element) return;
  for (const animation of activeMotion)
    if (animation.effect?.target === element) animation.cancel();
  if (
    !element.isConnected ||
    document.hidden ||
    matchMedia("(prefers-reduced-motion: reduce)").matches
  )
    return;
  const animation = element.animate(frames, {
    duration,
    easing: "cubic-bezier(.2,0,0,1)",
  });
  activeMotion.add(animation);
  const clean = () => activeMotion.delete(animation);
  animation.addEventListener("finish", clean, { once: true });
  animation.addEventListener("cancel", clean, { once: true });
  return animation;
}
export function decorateCheckbox(input) {
  if (input.nextElementSibling?.classList.contains("checkbox-art")) return;
  input.classList.add("motion-checkbox");
  const art = node("span", undefined, "checkbox-art");
  art.setAttribute("aria-hidden", "true");
  art.innerHTML =
    '<svg viewBox="0 0 24 24"><path pathLength="1" d="M5 12.5l4.6 4.6L19 7.4"/></svg>';
  const control = node("span", undefined, "checkbox-control");
  input.before(control);
  control.append(input, art);
}
export function reconcileChildren(host, children) {
  const active = host.contains(document.activeElement)
    ? document.activeElement
    : null;
  const keep = new Set(children);
  for (const child of Array.from(host.children))
    if (!keep.has(child)) child.remove();
  children.forEach((child, index) => {
    if (host.children[index] !== child)
      host.insertBefore(child, host.children[index] || null);
  });
  if (active?.isConnected && document.activeElement !== active)
    active.focus({ preventScroll: true });
}

export function beginLoading(host) {
  loading.get(host)?.finish();
  const placeholder = node("div", "正在读取…", "loading-placeholder");
  placeholder.setAttribute("role", "status");
  let timer;
  const request = {
    current: () => loading.get(host) === request,
    suspend: () => {
      clearTimeout(timer);
      placeholder.remove();
    },
    resume: () => {
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (request.current() && host.isConnected && !host.children.length)
          host.append(placeholder);
      }, 150);
    },
    finish: () => {
      clearTimeout(timer);
      placeholder.remove();
      if (request.current()) {
        loading.delete(host);
        host.removeAttribute("aria-busy");
      }
    },
  };
  loading.set(host, request);
  host.setAttribute("aria-busy", "true");
  request.resume();
  return request;
}

function setupTabMotion() {
  const calibrators = [];
  for (const group of document.querySelectorAll(".tabs,.mode-switch")) {
    const indicator = node("span", undefined, "tab-indicator");
    indicator.setAttribute("aria-hidden", "true");
    group.append(indicator);
    let intent = false,
      previous = null;
    const sync = (animate = false) => {
      const selected = group.querySelector(
        '[aria-selected="true"],[aria-pressed="true"]',
      );
      if (!selected || !selected.getClientRects().length) return;
      const next = {
        transform: `translateX(${selected.offsetLeft}px)`,
        width: selected.offsetWidth + "px",
      };
      if (
        previous?.transform === next.transform &&
        previous?.width === next.width
      )
        return;
      indicator.style.transform = next.transform;
      indicator.style.width = next.width;
      indicator.style.top =
        selected.offsetTop + selected.offsetHeight - 2 + "px";
      if (animate && previous) animateOnce(indicator, [previous, next], 240);
      else for (const a of indicator.getAnimations()) a.cancel();
      previous = next;
    };
    group.addEventListener(
      "click",
      () => {
        intent = true;
      },
      true,
    );
    group.addEventListener("click", () => {
      sync(true);
      intent = false;
    });
    const observer = new MutationObserver(() => sync(intent));
    observer.observe(group, {
      subtree: true,
      attributes: true,
      attributeFilter: ["aria-selected", "aria-pressed"],
    });
    const resize = new ResizeObserver(() => sync());
    resize.observe(group);
    group.querySelectorAll("button").forEach((b) => resize.observe(b));
    calibrators.push(sync);
    sync();
  }
  const syncAll = () => calibrators.forEach((sync) => sync());
  document.fonts.ready.then(syncAll);
  window.addEventListener("pageshow", syncAll);
  window.addEventListener("resize", syncAll);
  return syncAll;
}

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
  pendingNodes = () => [],
}) {
  function notice(message, error = false) {
    $("notice").inert = false;
    $("notice").hidden = false;
    const dismiss = node("button");
    dismiss.setAttribute("aria-label", "关闭消息");
    dismiss.innerHTML =
      '<svg class="control-icon" viewBox="0 0 20 20" aria-hidden="true"><path d="m6 6 8 8M14 6l-8 8"/></svg>';
    dismiss.addEventListener("click", () => {
      $("notice").inert = true;
      $("notice").hidden = true;
    });
    $("notice").replaceChildren(node("span", message), dismiss);
    $("notice").classList.toggle("error", error);
    animateOnce(
      $("notice"),
      [
        { opacity: 0, translate: "0 -16px" },
        { opacity: 1, translate: "0 0" },
      ],
      260,
    );
  }

  function drawGraph(container, display, facts = [], options = {}) {
    const viewport = container._viewport || {
      zoom: 1,
      left: container.scrollLeft,
      top: container.scrollTop,
    };
    RunCradleGraph.draw(
      container,
      display,
      selectWorkbenchNode,
      facts,
      container.id === "editor-graph" ? { run: launch, add: addNode } : options,
    );
    container._display = display;
    RunCradleGraph.setZoom(container, viewport.zoom);
    container._viewport = viewport;
    requestAnimationFrame(() => restoreCanvas(container));
    updateCanvas();
  }

  function resetCanvas(container) {
    container._graphIdentity = (container._graphIdentity || 0) + 1;
    container._viewport = { zoom: 1, left: 0, top: 0 };
    RunCradleGraph.setZoom(container, 1);
    container.scrollTo(0, 0);
  }
  function restoreCanvas(container) {
    if (container.hidden || !container._viewport) return;
    container.scrollTo(container._viewport.left, container._viewport.top);
    zoomLabel();
  }
  function zoomLabel() {
    $("zoom-level").textContent =
      Math.round((Number(currentCanvas().dataset.zoom) || 1) * 100) + "%";
  }
  function zoom(value) {
    const canvas = currentCanvas();
    RunCradleGraph.setZoom(canvas, value);
    canvas._viewport = {
      zoom: Number(canvas.dataset.zoom),
      left: canvas.scrollLeft,
      top: canvas.scrollTop,
    };
    zoomLabel();
  }
  function markPending() {
    const pending = new Set(pendingNodes().map((n) => n.id));
    for (const group of $("editor-graph").querySelectorAll(".graph-node")) {
      const existing = group.querySelector(".node-unapplied");
      if (!pending.has(group.dataset.node)) {
        existing?.remove();
        continue;
      }
      if (existing) continue;
      const badge = document.createElementNS(
        "http://www.w3.org/2000/svg",
        "text",
      );
      badge.setAttribute("class", "node-unapplied");
      badge.setAttribute("x", "226");
      badge.setAttribute("y", "103");
      badge.setAttribute("text-anchor", "end");
      badge.textContent = "未应用修改";
      group.append(badge);
      animateOnce(badge, [{ opacity: 0.25 }, { opacity: 1 }], 220);
    }
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
      for (const [e, disabled] of viewState.disabledBefore)
        e.disabled = disabled;
      viewState.disabledBefore = [];
      $("version-select").disabled = !readEditor().workflow;
      $("apply-field").disabled =
        !readEditor().document?.valid ||
        readEditor().document?.display.view.raw_edit_only;
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
      dialog._returnFocus = document.activeElement.closest("#more-actions")
        ? $("more-actions").querySelector("summary")
        : document.activeElement;
      dialog.inert = false;
      dialog.showModal();
    }
  }

  function closeDialog(id) {
    if ($(id).open) {
      $(id).inert = true;
      $(id).close();
    }
  }

  function setMode(value) {
    rememberInspector();
    const old = currentCanvas();
    if (!old.hidden)
      old._viewport = {
        zoom: Number(old.dataset.zoom) || 1,
        left: old.scrollLeft,
        top: old.scrollTop,
      };
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
    restoreCanvas(currentCanvas());
    updateCanvas();
  }

  function updateCanvas() {
    markPending();
    zoomLabel();
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
    for (const b of $("workflow-list").querySelectorAll(
      "button[data-workflow]",
    )) {
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
    const syncTabs = setupTabMotion();
    bindListFeedback();
    bindHelp(showDialog);
    for (const details of document.querySelectorAll(
      "details:not(.toolbar-menu)",
    )) {
      const syncDisclosure = () => {
        const summary = details.querySelector(":scope > summary");
        for (const child of details.children) {
          if (child === summary) continue;
          if (!details.open && child.contains(document.activeElement))
            summary?.focus();
          child.inert = !details.open;
        }
      };
      syncDisclosure();
      new MutationObserver(syncDisclosure).observe(details, {
        attributes: true,
        attributeFilter: ["open"],
        childList: true,
      });
    }
    document
      .querySelectorAll('input[type="checkbox"]')
      .forEach(decorateCheckbox);
    document.addEventListener("change", (event) => {
      const input = event.target;
      if (!input.matches(".motion-checkbox")) return;
      const art = input.nextElementSibling;
      animateOnce(
        art,
        input.checked
          ? [
              { transform: "scale(1)" },
              { transform: "scale(1.1)", offset: 0.4 },
              { transform: "scale(1)" },
            ]
          : [{ opacity: 0.6 }, { opacity: 1 }],
        input.checked ? 260 : 160,
      );
      if (input.checked)
        animateOnce(
          art.querySelector("path"),
          [{ strokeDashoffset: 1 }, { strokeDashoffset: 0 }],
          180,
        );
    });
    document.addEventListener(
      "click",
      (event) => {
        const button = event.target.closest("button");
        if (!button || button.disabled) return;
        animateOnce(
          button,
          [{ scale: ".97" }, { scale: "1.02", offset: 0.55 }, { scale: "1" }],
          220,
        );
      },
      true,
    );
    const resetMotion = () => {
      for (const animation of activeMotion) animation.cancel();
      activeMotion.clear();
    };
    const suspendMotion = () => {
      resetMotion();
      for (const request of loading.values()) request.suspend();
    };
    window.addEventListener("pagehide", suspendMotion);
    window.addEventListener("pageshow", () => {
      resetMotion();
      for (const request of loading.values()) request.resume();
    });
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) suspendMotion();
      else for (const request of loading.values()) request.resume();
    });
    matchMedia("(prefers-reduced-motion: reduce)").addEventListener(
      "change",
      resetMotion,
    );
    if (matchMedia("(max-width:700px)").matches)
      document.body.classList.add("sidebar-collapsed");
    for (const d of document.querySelectorAll("dialog")) {
      d.addEventListener("close", () => {
        // A queued close event can arrive after a rapid reopen.
        if (!d.open) {
          d.inert = true;
          d._returnFocus?.focus();
        }
      });
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
    on("toggle-sidebar", () => {
      const closed = document.body.classList.toggle("sidebar-collapsed");
      if (!closed)
        animateOnce(
          $("sidebar"),
          [
            { opacity: 0, translate: "-24px 0" },
            { opacity: 1, translate: "0 0" },
          ],
          280,
        );
    });
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
      syncTabs();
      if (!$("console-body").hidden)
        animateOnce(
          $("console-body"),
          [
            { opacity: 0, translate: "0 12px" },
            { opacity: 1, translate: "0 0" },
          ],
          260,
        );
      $("toggle-console").setAttribute(
        "aria-expanded",
        String(!$("console-body").hidden),
      );
    });
    for (const tab of ["overview", "logs", "details"]) {
      const button = $("evidence-" + tab + "-tab");
      button.tabIndex = tab === "overview" ? 0 : -1;
      button.addEventListener("keydown", (event) => {
        const names = ["overview", "logs", "details"];
        const index = names.indexOf(tab);
        const next = {
          ArrowRight: (index + 1) % 3,
          ArrowLeft: (index + 2) % 3,
          Home: 0,
          End: 2,
        }[event.key];
        if (next === undefined) return;
        event.preventDefault();
        const target = $("evidence-" + names[next] + "-tab");
        target.focus();
        target.click();
      });
      on(button.id, () => {
        const changed = $("evidence-" + tab).hidden;
        for (const name of ["overview", "logs", "details"]) {
          $("evidence-" + name).hidden = name !== tab;
          $("evidence-" + name + "-tab").tabIndex = name === tab ? 0 : -1;
          $("evidence-" + name + "-tab").setAttribute(
            "aria-selected",
            String(name === tab),
          );
        }
        if (changed)
          animateOnce($("evidence-" + tab), [{ opacity: 0 }, { opacity: 1 }]);
      });
    }
    for (const [id, panel] of [
      ["library-tab", "library"],
      ["history-tab", "history"],
    ])
      on(id, () => {
        const changed = $(panel + "-panel").hidden;
        $("library-panel").hidden = panel !== "library";
        $("history-panel").hidden = panel !== "history";
        if (changed)
          animateOnce($(panel + "-panel"), [{ opacity: 0 }, { opacity: 1 }]);
        $("library-tab").setAttribute(
          "aria-pressed",
          String(panel === "library"),
        );
        $("history-tab").setAttribute(
          "aria-pressed",
          String(panel === "history"),
        );
      });
    on("zoom-in", () =>
      zoom((Number(currentCanvas().dataset.zoom) || 1) * 1.2),
    );
    on("zoom-out", () =>
      zoom((Number(currentCanvas().dataset.zoom) || 1) / 1.2),
    );
    on("fit", () => {
      const canvas = currentCanvas(),
        graph = currentDisplay()?.graph;
      if (graph) {
        zoom(
          Math.min(
            1,
            (canvas.clientWidth - 40) / graph.width,
            (canvas.clientHeight - 114) / graph.height,
          ),
        );
        canvas.scrollTo(0, 0);
      }
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
    $("more-actions").addEventListener("click", (event) => {
      if (event.target.closest("button")) $("more-actions").open = false;
    });
    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && $("more-actions").open) {
        $("more-actions").open = false;
        $("more-actions").querySelector("summary").focus();
      }
    });
    const cycleTheme = RunCradleTheme.mount("visual-style");
    $("minimap").hidden = matchMedia("(max-width: 700px)").matches;
    for (const canvas of [$("editor-graph"), $("run-graph")]) {
      canvas.addEventListener("scroll", () => {
        if (!canvas.hidden)
          canvas._viewport = {
            zoom: Number(canvas.dataset.zoom) || 1,
            left: canvas.scrollLeft,
            top: canvas.scrollTop,
          };
      });
    }
    matchMedia("(max-width: 1250px)").addEventListener("change", () => {
      inspectorPresentation(!$("inspector").hidden);
    });
    matchMedia("(max-width: 700px)").addEventListener("change", (event) => {
      if (event.matches) document.body.classList.add("sidebar-collapsed");
      $("minimap").hidden = event.matches;
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
      if (e.key === "t") {
        e.preventDefault();
        cycleTheme();
        return;
      }
      const keys = {
        "+": "zoom-in",
        "=": "zoom-in",
        "-": "zoom-out",
        0: "fit",
        m: "map-toggle",
        "[": "toggle-sidebar",
      };
      if (keys[e.key]) {
        e.preventDefault();
        $(keys[e.key]).click();
      }
    });
    window.addEventListener("pageshow", () => restoreCanvas(currentCanvas()));
  }
  return {
    resetCanvas,
    markPending,
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
