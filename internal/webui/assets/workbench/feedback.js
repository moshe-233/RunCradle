// Online-only interaction feedback. Business outcomes are supplied by callers.
const $ = (id) => document.getElementById(id);
const element = (tag, text, cls) => {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  if (cls) e.className = cls;
  return e;
};
export const resultLabel = (value) =>
  ({ pending: "待定", succeeded: "成功", failed: "失败", unknown: "未知" })[
    value
  ] ||
  value ||
  "未知";
export function formatTime(value) {
  const date = new Date(value);
  return value && Number.isFinite(date.getTime())
    ? date.toLocaleString("zh-CN", { hour12: false })
    : "时间未知";
}
export function shortTime(value) {
  const date = new Date(value);
  if (!value || !Number.isFinite(date.getTime())) return "时间未知";
  const two = (part) => String(part).padStart(2, "0");
  return `${date.getMonth() + 1}/${date.getDate()} ${two(date.getHours())}:${two(date.getMinutes())}`;
}
export function recordRow(
  existing,
  { id, kind, title, detail, result, search = "", fullTime = "" },
  select,
) {
  let row = existing;
  if (!row) {
    row = element("div", undefined, "resource-row");
    row.dataset.record = id;
    const button = element("button", undefined, "record-select");
    button.dataset[kind] = id;
    button.append(element("strong"), element("small"));
    button.addEventListener("click", select);
    const copy = element("button", undefined, "record-copy");
    copy.innerHTML =
      '<svg class="control-icon" viewBox="0 0 20 20" aria-hidden="true"><rect x="7" y="6" width="9" height="11" rx="1.5"/><path d="M4 13V4a1 1 0 0 1 1-1h8"/></svg>';
    copy.setAttribute("aria-label", "复制完整标识 " + id);
    copy.title = "复制完整标识";
    copy.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(id);
        copy.title = "已复制";
        copy.setAttribute("aria-label", "已复制完整标识 " + id);
      } catch {
        copy.title = "复制失败，请重试";
        copy.setAttribute("aria-label", "复制失败，请重试 " + id);
      }
    });
    row.append(button, copy);
  }
  const button = row.querySelector(".record-select");
  if (button.querySelector("strong").textContent !== title)
    button.querySelector("strong").textContent = title;
  button.querySelector("strong").title = title;
  if (button.querySelector("small").textContent !== detail)
    button.querySelector("small").textContent = detail;
  button.querySelector("small").title = fullTime;
  button.dataset.result = result || "";
  row.dataset.result = result || "";
  row.dataset.search = [title, detail, fullTime, id, result, search]
    .join(" ")
    .toLowerCase();
  return row;
}
function stateBox(id, message, action, label) {
  const box = $(id);
  if (!box) return;
  // Repeated snapshots must not replace a focused retry/clear action.
  box._action = action;
  const signature = JSON.stringify([message, label]);
  if (box.dataset.signature === signature) return;
  box.dataset.signature = signature;
  box.replaceChildren(element("p", message));
  box.hidden = !message;
  if (action) {
    const button = element("button", label);
    button.addEventListener("click", () => box._action?.());
    box.append(button);
  }
}
export function resourceFilters() {
  const query = $("library-filter").value.trim().toLowerCase();
  $("clear-library-filter").hidden =
    !query && !$("history-result-filter").value;
  for (const [id, emptyText, action, actionText] of [
    [
      "workflow-list",
      "还没有保存的工作流。",
      () => $("new-workflow").click(),
      "新建工作流",
    ],
    [
      "run-history",
      "当前工作流还没有运行记录。",
      () => $("open-launchpad").click(),
      "打开运行启动台",
    ],
  ]) {
    const host = $(id),
      rows = [...host.querySelectorAll(".resource-row")];
    const result = id === "run-history" ? $("history-result-filter").value : "";
    rows.forEach((row) => {
      row
        .querySelector(".record-select")
        .setAttribute(
          "aria-current",
          String(
            row.dataset.record ===
              $(id === "workflow-list" ? "workflow-id" : "run-id").textContent,
          ),
        );
      row.hidden =
        !row.dataset.search.includes(query) ||
        (!!result && row.dataset.result !== result);
    });
    const visible = rows.filter((row) => !row.hidden).length;
    $(id + "-count").textContent =
      `当前显示 ${visible} / 已加载 ${rows.length}`;
    const failed = host.dataset.readError;
    const busy = host.getAttribute("aria-busy") === "true";
    if (failed)
      stateBox(id + "-state", "读取失败：" + failed, host._retry, "重试读取");
    else if (busy && !rows.length) stateBox(id + "-state", "正在读取…");
    else if (!rows.length)
      stateBox(id + "-state", emptyText, action, actionText);
    else if (!visible)
      stateBox(
        id + "-state",
        "没有匹配的记录。筛选仅作用于已加载记录。",
        clearResourceFilter,
        "清空筛选",
      );
    else stateBox(id + "-state", "");
  }
}
export function clearResourceFilter() {
  $("library-filter").value = "";
  $("history-result-filter").value = "";
  resourceFilters();
}
export function eventFilters() {
  const host = $("timeline"),
    rows = [...host.children],
    nodeFilter = $("event-node-filter");
  const selected = nodeFilter.value,
    names = [
      ...new Set(rows.map((row) => row.dataset.node).filter(Boolean)),
    ].sort();
  if (selected && !names.includes(selected)) names.push(selected);
  const key = JSON.stringify(names);
  if (nodeFilter.dataset.options !== key) {
    nodeFilter.replaceChildren(
      new Option("全部节点", ""),
      ...names.map((name) => new Option(name, name)),
    );
    nodeFilter.value = selected;
    nodeFilter.dataset.options = key;
  }
  const query = $("event-filter").value.trim().toLowerCase();
  rows.forEach((row) => {
    row.hidden =
      !row.textContent.toLowerCase().includes(query) ||
      (!!selected && row.dataset.node !== selected);
  });
  const visible = rows.filter((row) => !row.hidden).length;
  $("event-count").textContent =
    `当前显示 ${visible} / 已缓存 ${rows.length} · 最近100条事件`;
  $("clear-event-filter").hidden = !query && !selected;
  stateBox(
    "event-list-state",
    rows.length
      ? visible
        ? ""
        : "没有匹配的事件。"
      : "尚无已缓存事件；历史查看不会补造事件正文。",
  );
}
export function bindListFeedback() {
  $("library-filter").addEventListener("input", resourceFilters);
  $("history-result-filter").addEventListener("change", resourceFilters);
  $("clear-library-filter").addEventListener("click", clearResourceFilter);
  $("event-filter").addEventListener("input", eventFilters);
  $("event-node-filter").addEventListener("change", eventFilters);
  $("clear-event-filter").addEventListener("click", () => {
    $("event-filter").value = "";
    $("event-node-filter").value = "";
    eventFilters();
  });
  resourceFilters();
  eventFilters();
}

// The lock is independent of the editor lock (stop is allowed during editing).
const operations = new Map();
export const operationPending = (id) => operations.get($(id))?.pending === true;
export async function withOperation(id, pendingText, action) {
  const button = $(id),
    previous = operations.get(button);
  if (previous?.pending) return;
  previous?.reset();
  const label = button.textContent,
    disabled = button.disabled;
  let spinner,
    restore,
    succeeded = false,
    feedbackEnded = false;
  const state = {
    pending: true,
    reset() {
      clearTimeout(spinner);
      clearTimeout(restore);
      if (operations.get(button) !== state) return;
      button.textContent = button.dataset.idleLabel || label;
      button.classList.remove("operation-spinning", "operation-success");
      button.removeAttribute("aria-busy");
      feedbackEnded = true;
      if (!state.pending) operations.delete(button);
    },
  };
  operations.set(button, state);
  button.textContent = pendingText;
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  spinner = setTimeout(() => {
    if (state.pending && !document.hidden)
      button.classList.add("operation-spinning");
  }, 150);
  try {
    return await action((successText) => {
      succeeded = true;
      clearTimeout(spinner);
      button.classList.remove("operation-spinning");
      button.classList.add("operation-success");
      button.textContent = successText;
      button.removeAttribute("aria-busy");
      restore = setTimeout(() => state.reset(), 1200);
    });
  } finally {
    state.pending = false;
    button.disabled = disabled;
    if (!succeeded) state.reset();
    else if (feedbackEnded) operations.delete(button);
  }
}

export function bindHelp(showDialog) {
  const tips = {
    fit: "适应整个工作流 · 0",
    "map-toggle": "显示或收起小地图 · M",
    "zoom-in": "放大画布 · +",
    "zoom-out": "缩小画布 · −",
    "trace-up": "突出所选节点的上游依赖",
    "trace-down": "突出所选节点的下游依赖",
    "trace-clear": "清除依赖追踪",
    "toggle-sidebar": "展开或收起资源栏 · [",
    "visual-style": "选择外观主题 · T 按列表顺序切换",
    "open-search": "搜索节点与操作 · Ctrl / ⌘ + K 或 /",
    "close-inspector": "关闭检查器；未应用输入会保留",
    "run-integrity": "历史完整性独立于任务结果；快照不能补齐事件缺口",
    "run-cleanup": "清理状态独立于任务结果与引擎退出",
    "retain-evidence": "仅留存实际失败线索；不会补造日志正文",
  };
  for (const [id, text] of Object.entries(tips)) {
    const target = $(id);
    target.dataset.tip = text;
    target.removeAttribute("title");
  }
  $("open-help").addEventListener("click", () => showDialog("help-dialog"));
  const tip = element("div", undefined, "workbench-tooltip");
  tip.id = "workbench-tooltip";
  tip.setAttribute("role", "tooltip");
  tip.hidden = true;
  const popover = "showPopover" in tip;
  if (popover) tip.setAttribute("popover", "manual");
  let target, hoverTimer, hideTimer;
  function hide() {
    clearTimeout(hoverTimer);
    clearTimeout(hideTimer);
    if (target) {
      const ids = (target.getAttribute("aria-describedby") || "")
        .split(/\s+/)
        .filter((id) => id && id !== tip.id);
      if (ids.length) target.setAttribute("aria-describedby", ids.join(" "));
      else target.removeAttribute("aria-describedby");
    }
    if (popover && tip.matches(":popover-open")) tip.hidePopover();
    tip.hidden = true;
    target = null;
  }
  function position() {
    if (!target?.isConnected || !target.getClientRects().length) return hide();
    const rect = target.getBoundingClientRect(),
      box = tip.getBoundingClientRect();
    tip.style.left =
      Math.max(
        8,
        Math.min(
          innerWidth - box.width - 8,
          rect.left + (rect.width - box.width) / 2,
        ),
      ) + "px";
    tip.style.top =
      Math.max(
        8,
        rect.bottom + box.height + 16 < innerHeight
          ? rect.bottom + 8
          : rect.top - box.height - 8,
      ) + "px";
  }
  function show(next) {
    hide();
    if (next.disabled) return;
    target = next;
    (next.closest("dialog[open]") || document.body).append(tip);
    tip.textContent = next.dataset.tip;
    tip.hidden = false;
    next.setAttribute(
      "aria-describedby",
      [next.getAttribute("aria-describedby"), tip.id].filter(Boolean).join(" "),
    );
    if (popover) tip.showPopover();
    position();
  }
  const laterHide = () => {
    clearTimeout(hideTimer);
    hideTimer = setTimeout(hide, 120);
  };
  document.addEventListener("pointerover", (event) => {
    if (event.pointerType === "touch" || !matchMedia("(hover:hover)").matches)
      return;
    const next = event.target.closest("[data-tip]");
    if (!next || next.contains(event.relatedTarget)) return;
    clearTimeout(hideTimer);
    clearTimeout(hoverTimer);
    hoverTimer = setTimeout(() => show(next), 300);
  });
  document.addEventListener("pointerout", (event) => {
    const next = event.target.closest("[data-tip]");
    if (next && !next.contains(event.relatedTarget)) {
      clearTimeout(hoverTimer);
      laterHide();
    }
  });
  tip.addEventListener("pointerenter", () => clearTimeout(hideTimer));
  tip.addEventListener("pointerleave", laterHide);
  document.addEventListener("focusin", (event) => {
    const next = event.target.closest("[data-tip]");
    if (next) show(next);
    else hide();
  });
  document.addEventListener("focusout", laterHide);
  document.addEventListener(
    "keydown",
    (event) => {
      if (event.key === "Escape" && !tip.hidden) {
        hide();
        event.preventDefault();
        event.stopImmediatePropagation();
      }
    },
    true,
  );
  document.addEventListener("close", hide, true);
  document.addEventListener(
    "scroll",
    () => {
      if (target === document.activeElement) position();
      else hide();
    },
    true,
  );
  window.addEventListener("resize", hide);
  document.addEventListener("visibilitychange", hide);
  window.addEventListener("pagehide", hide);
  window.addEventListener("pageshow", hide);
}

document.addEventListener("visibilitychange", () => {
  if (document.hidden)
    for (const [button, state] of operations) {
      button.classList.remove("operation-spinning");
      if (!state.pending) state.reset();
    }
});
window.addEventListener("pagehide", () => {
  for (const state of operations.values()) state.reset();
});

let errorSerial = 0;
export function focusField(field) {
  if (!field) return;
  const opened = [];
  for (let parent = field.parentElement; parent; parent = parent.parentElement)
    if (parent.matches("details") && !parent.open) {
      parent.dataset.errorReveal = "true";
      parent.open = true;
      opened.push(parent);
    }
  requestAnimationFrame(() => {
    if (!field.isConnected || field.closest("[hidden]")) {
      for (const details of opened) delete details.dataset.errorReveal;
      return;
    }
    field.focus({ preventScroll: true });
    const scroller = field.closest("#inspector,dialog");
    if (scroller) {
      const top =
        field.getBoundingClientRect().top -
        scroller.getBoundingClientRect().top +
        scroller.scrollTop;
      scroller.scrollTo({
        top: Math.max(0, top - (scroller.id === "inspector" ? 108 : 76)),
        behavior: "instant",
      });
    }
    requestAnimationFrame(() => {
      for (const details of opened) delete details.dataset.errorReveal;
    });
  });
}
export function clearFieldErrors(container) {
  for (const message of container.querySelectorAll(".field-error")) {
    const field = message._field;
    if (field) {
      field.removeAttribute("aria-invalid");
      const ids = (field.getAttribute("aria-describedby") || "")
        .split(/\s+/)
        .filter((id) => id && id !== message.id);
      if (ids.length) field.setAttribute("aria-describedby", ids.join(" "));
      else field.removeAttribute("aria-describedby");
    }
    message.remove();
  }
}
export function fieldErrors(
  container,
  summary,
  errors,
  { focus = false, announce = true, source } = {},
) {
  clearFieldErrors(container);
  summary.setAttribute("role", announce ? "alert" : "note");
  summary.replaceChildren();
  summary.hidden = !errors.length;
  if (!errors.length) return;
  const list = element("ul");
  summary.append(element("strong", `请检查 ${errors.length} 项问题`), list);
  for (const { field, message } of errors) {
    const item = element("li");
    list.append(item);
    if (field) {
      const text = element("span", message, "field-error");
      text.id = "field-error-" + ++errorSerial;
      text._field = field;
      const row = field.closest(".kv-row,.dep-row,.arg-row");
      if (row) row.append(text);
      else field.after(text);
      field.setAttribute("aria-invalid", "true");
      field.setAttribute(
        "aria-describedby",
        [field.getAttribute("aria-describedby"), text.id]
          .filter(Boolean)
          .join(" "),
      );
      const locate = element("button", message);
      locate.type = "button";
      locate.addEventListener("click", () => focusField(field));
      item.append(locate);
    } else item.textContent = message;
  }
  if (source) {
    const button = element("button", "查看源码与校验问题");
    button.addEventListener("click", source);
    summary.append(button);
  }
  if (focus) focusField(errors.find((error) => error.field)?.field);
}
