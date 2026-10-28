"use strict";
// Shared, authority-free canvas renderer: declarations + optional observed facts.
window.RunCradleGraph = (() => {
  const ns = "http://www.w3.org/2000/svg";
  const svgNode = (tag, attrs = {}, text) => {
    const n = document.createElementNS(ns, tag);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v));
    if (text !== undefined) n.textContent = text;
    return n;
  };
  function edgeState(edge, nodes, facts) {
    const a = facts.find((n) => n.name === edge.FromName),
      b = facts.find((n) => n.name === edge.ToName),
      decl = nodes.find((n) => n.name === edge.FromName);
    if (!a) return { state: "idle", reason: "未执行" };
    const started = (n) => !!n && (!!n.started_at || n.state === "running");
    if (started(b))
      return { state: "satisfied", reason: "由下游已启动推断引擎已放行" };
    const ended = !!a.ended_at || ["completed", "failed"].includes(a.state),
      success =
        a.exit_code !== null &&
        a.exit_code !== undefined &&
        (decl?.success_exit_codes || ["0"]).includes(String(a.exit_code)) &&
        !a.signal;
    let satisfied = false,
      known = true;
    switch (edge.Condition) {
      case "未显式指定":
      case "process_started":
        satisfied = started(a);
        break;
      case "process_completed":
        satisfied = ended;
        break;
      case "process_completed_successfully":
        satisfied = ended && success;
        known = !ended || (a.exit_code !== null && a.exit_code !== undefined);
        break;
      case "process_healthy":
        satisfied = a.health === "ready";
        known = a.health !== "unknown";
        break;
      case "process_log_ready":
        known = false;
        break;
      default:
        known = false;
    }
    if (satisfied) return { state: "satisfied", reason: "当前观察满足条件" };
    if (known && (a.state === "skipped" || ended))
      return { state: "blocked", reason: "前置已终止且条件未满足" };
    if (!known)
      return { state: "idle", reason: "证据不足：未取得此条件的独立事实" };
    return { state: started(a) ? "waiting" : "idle", reason: "等待前置条件" };
  }
  function draw(container, display, select, facts = [], actions = {}) {
    container.replaceChildren();
    if (!display?.graph) return;
    const graph = display.graph,
      nodes = display.view.nodes,
      svg = svgNode("svg", {
        viewBox: `0 0 ${graph.width} ${graph.height}`,
        width: graph.width,
        height: graph.height,
        role: "group",
        "aria-label": "工作流画布",
      });
    const defs = svgNode("defs"),
      marker = svgNode("marker", {
        id: container.id + "-arrow",
        viewBox: "0 0 10 10",
        refX: 9,
        refY: 5,
        markerWidth: 5,
        markerHeight: 5,
        orient: "auto-start-reverse",
      });
    marker.append(
      svgNode("path", { d: "M 0 0 L 10 5 L 0 10 z", fill: "currentColor" }),
    );
    defs.append(marker);
    svg.append(defs);
    for (const edge of graph.edges) {
      const status = edgeState(edge, nodes, facts),
        g = svgNode("g", {
          class: "edge " + status.state,
          "data-from": edge.FromID,
          "data-to": edge.ToID,
          "data-edge-state": status.state,
        });
      g.append(
        svgNode("title", {}, edge.Condition + " · " + status.reason),
        svgNode("path", {
          d: edge.Path,
          "marker-end": `url(#${container.id}-arrow)`,
        }),
        svgNode("rect", {
          x: edge.X - 49,
          y: edge.Y - 11,
          width: 98,
          height: 22,
          rx: 11,
        }),
        svgNode(
          "text",
          { x: edge.X, y: edge.Y + 4, "text-anchor": "middle" },
          edge.Label,
        ),
      );
      svg.append(g);
    }
    const short = (s, n = 27) =>
      Array.from(s || "")
        .slice(0, n)
        .join("") + (Array.from(s || "").length > n ? "…" : "");
    for (const p of graph.nodes) {
      const d = nodes.find((n) => n.id === p.ID),
        f = facts.find((n) => n.name === p.Name),
        g = svgNode("g", {
          class: "graph-node " + (f?.state || "declared"),
          "data-node": p.ID,
          role: "button",
          tabindex: 0,
          "aria-label": p.Label,
          transform: `translate(${p.X},${p.Y})`,
        });
      g.append(
        svgNode("title", {}, p.Label + " · " + (f?.state || "配置声明")),
        svgNode("rect", { width: 240, height: 104, rx: 10 }),
        svgNode("circle", { cx: 17, cy: 22, r: 4, class: "state-dot" }),
        svgNode(
          "text",
          { x: 29, y: 26, class: "node-title" },
          short(p.Label, 17),
        ),
        svgNode(
          "text",
          { x: 14, y: 44, class: "node-name" },
          short(p.Name, 20),
        ),
        svgNode(
          "text",
          {
            x: 226,
            y: 44,
            "text-anchor": "end",
            class: "node-role",
            "data-start": f?.started_at || "",
            "data-end": f?.ended_at || "",
          },
          f?.state || d?.role?.toUpperCase(),
        ),
        svgNode(
          "text",
          { x: 14, y: 64, class: "node-command" },
          short(d?.command_preview),
        ),
        svgNode(
          "text",
          { x: 14, y: 80, class: "node-param" },
          short(
            (d?.env_items || [])
              .filter((x) => x.bound_input)
              .map((x) => "◇ " + x.bound_input)
              .join(" · "),
          ),
        ),
      );
      const probe =
        d?.has_readiness_probe || d?.has_liveness_probe
          ? f?.health || "未观察"
          : "No Probe";
      const receipt = f
        ? f.signal
          ? "SIG " + f.signal
          : f.exit_code !== null && f.exit_code !== undefined
            ? "EXIT " + f.exit_code
            : "UNKNOWN"
        : "未执行";
      g.append(
        svgNode("text", { x: 14, y: 96, class: "node-probe" }, probe),
        svgNode(
          "text",
          { x: 226, y: 96, "text-anchor": "end", class: "node-exit" },
          receipt,
        ),
      );
      const activate = () => select?.(p.ID);
      g.addEventListener("click", activate);
      g.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          activate();
        }
      });
      if (actions.run) {
        const btn = svgNode("g", {
          class: "node-quick",
          role: "button",
          tabindex: 0,
          "aria-label": "运行到 " + p.Name,
        });
        btn.append(
          svgNode("rect", { x: 191, y: 7, width: 22, height: 20, rx: 4 }),
          svgNode("text", { x: 197, y: 22 }, "▶"),
        );
        const run = (e) => {
          e.stopPropagation();
          actions.run(p.Name);
        };
        btn.addEventListener("click", run);
        btn.addEventListener("keydown", (e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            run(e);
          }
        });
        g.append(btn);
      }
      if (actions.add) {
        const btn = svgNode("g", {
          class: "node-add",
          role: "button",
          tabindex: 0,
          "aria-label": "新增下游 " + p.Name,
        });
        btn.append(
          svgNode("circle", { cx: 240, cy: 52, r: 11 }),
          svgNode("text", { x: 236, y: 57 }, "+"),
        );
        const add = (e) => {
          e.stopPropagation();
          actions.add(p.Name);
        };
        btn.addEventListener("click", add);
        btn.addEventListener("keydown", (e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            add(e);
          }
        });
        g.append(btn);
      }
      svg.append(g);
    }
    container.append(svg);
    setZoom(container, Number(container.dataset.zoom) || 1);
    return svg;
  }
  function setZoom(container, value) {
    container.dataset.zoom = String(Math.max(0.15, Math.min(2, value)));
    const svg = container.querySelector("svg");
    if (svg) {
      const box = svg.viewBox.baseVal;
      svg.setAttribute("width", box.width * Number(container.dataset.zoom));
      svg.setAttribute("height", box.height * Number(container.dataset.zoom));
    }
  }
  function spotlight(container, display, id, direction) {
    const n = display?.view.nodes.find((n) => n.id === id),
      ids = new Set([id, ...(n?.[direction + "_ids"] || [])]);
    container.querySelectorAll("[data-node]").forEach((el) => {
      el.classList.toggle("dimmed", !!direction && !ids.has(el.dataset.node));
      el.classList.toggle("selected", el.dataset.node === id);
    });
    container
      .querySelectorAll(".edge")
      .forEach((el) =>
        el.classList.toggle(
          "dimmed",
          !!direction && !(ids.has(el.dataset.from) && ids.has(el.dataset.to)),
        ),
      );
  }
  function minimap(target, display, focus) {
    target.replaceChildren();
    if (!display?.graph) return;
    const g = display.graph,
      s = svgNode("svg", {
        viewBox: `0 0 ${g.width} ${g.height}`,
        width: 170,
        height: 90,
        "aria-label": "小地图",
      });
    for (const p of g.nodes) {
      const r = svgNode("rect", {
        x: p.X,
        y: p.Y,
        width: 240,
        height: 104,
        rx: 10,
        tabindex: 0,
        role: "button",
        "aria-label": p.Label,
      });
      r.addEventListener("click", () => focus(p.ID));
      r.addEventListener("keydown", (e) => {
        if (e.key === "Enter") focus(p.ID);
      });
      s.append(r);
    }
    target.append(s);
  }
  function lineDiff(left, right) {
    const a = left.split("\n"),
      b = right.split("\n"),
      result = {
        left: a.map((text) => ({ text, changed: true })),
        right: b.map((text) => ({ text, changed: true })),
        limited: false,
      };
    let start = 0;
    while (start < a.length && start < b.length && a[start] === b[start]) {
      result.left[start].changed = result.right[start].changed = false;
      start++;
    }
    let endA = a.length,
      endB = b.length;
    while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
      result.left[--endA].changed = result.right[--endB].changed = false;
    }
    const n = endA - start,
      m = endB - start;
    // Bound quadratic work on large raw documents; retain complete text with an explicit coarse diff.
    if (n * m > 1000000) {
      result.limited = true;
      return result;
    }
    const table = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
    for (let i = n - 1; i >= 0; i--)
      for (let j = m - 1; j >= 0; j--)
        table[i][j] =
          a[start + i] === b[start + j]
            ? table[i + 1][j + 1] + 1
            : Math.max(table[i + 1][j], table[i][j + 1]);
    let i = 0,
      j = 0;
    while (i < n && j < m) {
      if (a[start + i] === b[start + j]) {
        result.left[start + i++].changed = false;
        result.right[start + j++].changed = false;
      } else if (table[i + 1][j] >= table[i][j + 1]) i++;
      else j++;
    }
    return result;
  }
  return { draw, setZoom, spotlight, minimap, edgeState, lineDiff };
})();
