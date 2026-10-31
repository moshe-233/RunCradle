"use strict";
(() => {
  const $ = (id) => document.getElementById(id),
    payload = JSON.parse($("workflow-data").textContent),
    nodes = payload.view.nodes;
  const element = (tag, text, cls) => {
    const e = document.createElement(tag);
    if (text !== undefined) e.textContent = text;
    if (cls) e.className = cls;
    return e;
  };
  let selectedID = nodes[0]?.id,
    direction = "",
    automaticZoom = true;
  $("native-source").textContent = payload.source;
  if ($("ui-source")) $("ui-source").textContent = payload.ui_source;
  function select(id) {
    selectedID = id;
    const n = nodes.find((n) => n.id === id);
    if (!n) return;
    $("node-detail").replaceChildren(
      element("h3", n.label),
      element("p", n.name),
      element("p", "未执行 · 配置声明"),
    );
    for (const [k, v] of Object.entries(n.declaration_text))
      $("node-detail").append(element("h4", k), element("pre", v));
    document
      .querySelectorAll(".step")
      .forEach((e) =>
        e.setAttribute("aria-current", String(e.dataset.node === id)),
      );
    RunCradleGraph.spotlight($("canvas"), payload, id, direction);
  }
  RunCradleGraph.draw($("canvas"), payload, select);
  $("canvas").querySelector("svg").id = "graph";
  RunCradleGraph.minimap($("offline-map"), payload, (id) => {
    select(id);
    $("canvas")
      .querySelector(`[data-node="${id}"]`)
      ?.scrollIntoView({ block: "nearest", inline: "center" });
  });
  document.querySelectorAll(".step").forEach((e) => {
    e.addEventListener("click", () => select(e.dataset.node));
    e.addEventListener("keydown", (event) => {
      const delta = ["ArrowDown", "ArrowRight"].includes(event.key)
        ? 1
        : ["ArrowUp", "ArrowLeft"].includes(event.key)
          ? -1
          : 0;
      if (delta) {
        event.preventDefault();
        const i = nodes.findIndex((n) => n.id === e.dataset.node),
          n = nodes[(i + delta + nodes.length) % nodes.length];
        select(n.id);
        document.querySelector(`.step[data-node="${n.id}"]`).focus();
      }
    });
  });
  const list = $("input-list");
  for (const input of payload.metadata.inputs || []) {
    const e = element("div", undefined, "input-item");
    e.append(
      element("h3", input.label || input.name),
      element("p", `${input.type} · ${input.default_text ?? "必需，无默认值"}`),
    );
    for (const b of input.bindings)
      e.append(element("p", `${b.process} → environment.${b.key}`));
    list.append(e);
  }
  for (const preset of payload.metadata.presets || [])
    list.append(
      element("h3", preset.label || preset.id),
      element("pre", preset.values_text),
    );
  const on = (id, fn) => $(id).addEventListener("click", fn),
    zoom = (factor) =>
      RunCradleGraph.setZoom(
        $("canvas"),
        (Number($("canvas").dataset.zoom) || 1) * factor,
      );
  const fit = () =>
    RunCradleGraph.setZoom(
      $("canvas"),
      Math.min(1, ($("canvas").clientWidth - 40) / payload.graph.width),
    );
  on("zoom-in", () => {
    automaticZoom = false;
    zoom(1.2);
  });
  on("zoom-out", () => {
    automaticZoom = false;
    zoom(1 / 1.2);
  });
  on("fit", () => {
    automaticZoom = true;
    fit();
  });
  new ResizeObserver(() => {
    if (automaticZoom) fit();
  }).observe($("canvas"));
  fit();
  on("offline-map-toggle", () => {
    $("offline-map").hidden = !$("offline-map").hidden;
  });
  const cycleTheme = RunCradleTheme.mount("offline-palette");
  for (const [id, dir] of [
    ["offline-up", "upstream"],
    ["offline-down", "downstream"],
    ["offline-clear", ""],
  ])
    on(id, () => {
      direction = dir;
      select(selectedID);
    });
  // contenteditable search is a local navigation surface, never a run parameter form.
  const search = $("offline-search");
  search.addEventListener("input", () => {
    const q = search.textContent.toLowerCase();
    for (const e of document.querySelectorAll(".step")) {
      const n = nodes.find((n) => n.id === e.dataset.node);
      e.hidden = ![
        n.name,
        n.label,
        n.command_preview,
        ...n.env_items.map((e) => e.key + "=" + e.value),
      ]
        .join(" ")
        .toLowerCase()
        .includes(q);
    }
  });
  document.addEventListener("keydown", (e) => {
    if (e.target.isContentEditable || e.target.matches("input,textarea,select"))
      return;
    if (e.key === "t") {
      e.preventDefault();
      cycleTheme();
      return;
    }
    const keys = {
      "+": "zoom-in",
      "-": "zoom-out",
      0: "fit",
      m: "offline-map-toggle",
    };
    if (e.key === "/" || ((e.ctrlKey || e.metaKey) && e.key === "k")) {
      e.preventDefault();
      search.focus();
    } else if (keys[e.key]) {
      e.preventDefault();
      $(keys[e.key]).click();
    }
  });
  if (nodes.length) select(selectedID);
})();
