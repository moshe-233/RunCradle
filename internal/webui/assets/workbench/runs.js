import { renderFact } from "./inspector.js";
import {
  resourceFilters,
  recordRow,
  formatTime,
  shortTime,
  resultLabel,
  eventFilters,
  withOperation,
  operationPending,
  fieldErrors,
  clearFieldErrors,
} from "./feedback.js";
import {
  $,
  node,
  reconcileChildren,
  beginLoading,
  decorateCheckbox,
} from "./view.js";

// 运行观察持有自己的 SSE 与刷新定时器，不修改编辑草稿。
export function createRuns({
  runsState,
  api,
  closeDialog,
  dirty,
  drawGraph,
  encodedValues,
  parameterErrors = () => [],
  focusNode,
  guard,
  setMode,
  notice,
  on,
  readEditor,
  readView,
  renderNodeFacts,
  selectedNode,
  showDialog,
  syncButtons,
  pendingNodes,
  returnToEditing,
  resetCanvas,
}) {
  let targetNodes = [],
    targetNames = [],
    targetNotice = "";
  let eventCursor = 0;
  const selectedTargets = () => [...targetNames];
  function syncTargets(nodes, { reset = false } = {}) {
    targetNodes = nodes;
    const previous = targetNames;
    targetNames = reset
      ? []
      : targetNames.filter((name) => nodes.some((n) => n.name === name));
    if (reset) {
      targetNotice = "";
      $("target-search").value = "";
    } else if (previous.length !== targetNames.length) {
      targetNotice =
        "已移除删除或改名的目标：" +
        previous.filter((name) => !targetNames.includes(name)).join("、") +
        "。请重新确认目标。";
      notice(targetNotice);
    }
    renderTargets();
  }
  function renderTargets() {
    const query = $("target-search").value.trim().toLowerCase();
    $("run-targets").replaceChildren();
    for (const n of targetNodes) {
      if (!`${n.label} ${n.name}`.toLowerCase().includes(query)) continue;
      const row = node("label", undefined, "target-choice");
      const input = node("input");
      input.type = "checkbox";
      input.value = n.name;
      input.checked = targetNames.includes(n.name);
      const text = node("span");
      text.append(node("strong", n.label), node("code", n.name));
      row.append(input, text);
      decorateCheckbox(input);
      input.addEventListener("change", () => {
        targetNames = input.checked
          ? [...targetNames, n.name]
          : targetNames.filter((name) => name !== n.name);
        launchFeedback();
        syncButtons();
      });
      $("run-targets").append(row);
    }
    if (!$("run-targets").children.length)
      $("run-targets").append(node("p", "没有匹配的节点。"));
    launchFeedback();
  }
  function launchFeedback() {
    $("target-summary").textContent = targetNames.length
      ? `已选 ${targetNames.length} 个目标：${targetNames.join("、")}`
      : "请选择至少一个目标节点。";
    $("target-notice").textContent = targetNotice;
    $("target-notice").hidden = !targetNotice;
    const count = pendingNodes().length;
    $("launch-pending").hidden = !count;
    $("launch-pending-text").textContent =
      `${count} 个节点有未应用输入，这些输入不会进入本次运行。`;
    $("launch-note").textContent = dirty()
      ? "请先保存草稿，再运行。"
      : "运行已保存的版本 " + readEditor().savedRevision.slice(0, 12);
  }

  function closeStream() {
    // 只释放页面观察资源；业务任务继续由宿主持有。
    if (runsState.stream) runsState.stream.close();
    runsState.stream = null;
    if (runsState.refreshTimer) clearTimeout(runsState.refreshTimer);
    runsState.refreshTimer = null;
  }

  function facts(snapshot) {
    if (snapshot.run_id !== runsState.activeRun) return;
    const animateFacts =
      runsState.renderedRun === snapshot.run_id && snapshot.owned !== false;
    runsState.snapshot = snapshot;
    $("run-graph").classList.toggle(
      "no-motion",
      snapshot.final || snapshot.owned === false,
    );
    runsState.renderedRun = snapshot.run_id;
    $("run-hud").textContent =
      `${snapshot.run_id.slice(0, 8)} · ${snapshot.task_result} · ${(snapshot.nodes || []).filter((n) => n.ended_at).length}/${snapshot.nodes?.length || 0} 已结束`;
    renderNodeFacts();
    $("run-empty").hidden = true;
    $("run-id").textContent = snapshot.run_id;
    $("run-revision").textContent = snapshot.revision;
    $("run-status").textContent = snapshot.task_result || "unknown";
    $("run-status").dataset.result = snapshot.task_result || "unknown";
    const failures = (snapshot.nodes || []).filter((n) => n.state === "failed");
    $("summary-targets").textContent =
      (snapshot.target_nodes || []).join("、") || "未知";
    $("summary-result").textContent =
      resultLabel(snapshot.task_result) +
      " · " +
      (snapshot.task_result || "unknown");
    $("summary-result").dataset.result = snapshot.task_result || "unknown";
    $("summary-anomalies").textContent =
      `${failures.length} 个失败节点 · ${(snapshot.nodes || []).filter((n) => n.health === "unhealthy").length} 个健康异常`;
    $("fact-controls").hidden = false;
    $("run-overview-summary").hidden = false;
    $("run-engine").textContent =
      snapshot.engine_exit_code == null
        ? `未知${snapshot.engine_signal ? " · signal " + snapshot.engine_signal : ""}`
        : String(snapshot.engine_exit_code);
    $("run-integrity").textContent = runsState.gap
      ? `${snapshot.history_integrity} · 连接或游标缺口已观察到`
      : snapshot.history_integrity;
    $("run-cleanup").textContent = snapshot.cleanup;
    $("run-scope").textContent = snapshot.cleanup_scope || "未知";
    $("run-io").textContent = snapshot.io_status;
    $("run-reason").textContent = snapshot.reason;
    $("stop-run").disabled =
      operationPending("stop-run") ||
      snapshot.final ||
      snapshot.owned === false;
    $("run-values").textContent =
      snapshot.values_text || "旧记录未提供实例值文本。";
    const oldFacts = new Map(
      Array.from($("run-nodes").querySelectorAll(".node-fact"), (el) => [
        el.dataset.factName,
        el,
      ]),
    );
    const rows = [];
    const priority = (n) =>
      n.state === "failed"
        ? (snapshot.target_nodes || []).includes(n.name)
          ? 2
          : 1
        : 0;
    const orderedFacts = [...(snapshot.nodes || [])].sort(
      (a, b) => priority(b) - priority(a),
    );
    for (const n of orderedFacts) {
      let item = oldFacts.get(n.name);
      if (!item) {
        item = node("div", undefined, "node-fact");
        item.dataset.factName = n.name;
        item.append(node("strong", n.name));
        const declared = runsState.runDefinition?.display.view.nodes.find(
          (d) => d.name === n.name,
        );
        if (declared) {
          const locate = node("button", "定位节点 " + n.name);
          locate.addEventListener("click", () => {
            setMode("monitor");
            focusNode(declared.id);
          });
          item.append(locate);
        }
      }
      item.classList.toggle("alert", n.state === "failed");
      item.dataset.state = n.state;
      renderFact(item, n);
      rows.push(item);
    }
    if (snapshot.operation_error)
      rows.push(node("p", `操作错误：${snapshot.operation_error}`, "muted"));
    if (snapshot.residual_path)
      rows.push(node("p", `残留现场：${snapshot.residual_path}`, "muted"));
    reconcileChildren($("run-nodes"), rows);
    filterFacts();
    if (runsState.runDefinition)
      drawGraph(
        $("run-graph"),
        runsState.runDefinition.display,
        snapshot.nodes || [],
        { animateFacts },
      );
  }

  function filterFacts() {
    const rows = [...$("run-nodes").querySelectorAll(".node-fact")],
      failed = $("fact-filter").value === "failed";
    for (const row of rows)
      row.hidden = failed && row.dataset.state !== "failed";
    const count = rows.filter((row) => !row.hidden).length;
    $("fact-count").textContent = `当前显示 ${count} / 快照节点 ${rows.length}`;
    $("fact-empty").hidden = !!count || !rows.length;
  }

  async function refreshRun(id) {
    const snapshot = await api(`runs/${id}`);
    if (runsState.activeRun !== id) return;
    facts(snapshot);
    if (snapshot.final) {
      closeStream();
      $("stream-state").textContent = "终态已保存；历史完整性仍按记录显示。";
      await runHistory();
    }
  }

  async function openRun(id) {
    const sameRun = runsState.activeRun === id;
    if (!sameRun) {
      resetCanvas($("run-graph"));
      $("event-filter").value = "";
      $("event-node-filter").value = "";
    }
    runsState.renderedRun = "";
    if (!sameRun) {
      $("fact-filter").value = "";
      $("fact-controls").hidden = true;
      $("fact-empty").hidden = true;
      $("stop-run").disabled = true;
      $("run-id").textContent = "";
      $("run-revision").textContent = "";
      $("run-status").textContent = "读取中";
      $("run-status").dataset.result = "";
      $("run-overview-summary").hidden = true;
      $("run-hud").textContent = "正在读取所选运行…";
      for (const key of [
        "run-engine",
        "run-integrity",
        "run-cleanup",
        "run-scope",
        "run-io",
        "run-reason",
      ])
        $(key).textContent = "未知";
      // Keep the same run's canvas during reload so an empty layout cannot erase its pan.
      if (!sameRun) $("run-graph").replaceChildren();
      $("run-nodes").replaceChildren();
      $("run-values").textContent = "";
      $("run-source").textContent = "";
    }
    closeStream();
    runsState.activeRun = id;
    resourceFilters();
    if (!sameRun) {
      eventCursor = 0;
      runsState.gap = false;
      runsState.runDefinition = null;
      $("timeline").replaceChildren();
    }
    eventFilters();
    $("stream-state").textContent = "正在读取所选运行及其版本…";
    const request = beginLoading($("run-nodes"));
    let pair;
    try {
      pair = await Promise.all([
        api(`runs/${id}`),
        api(`runs/${id}/definition`),
      ]);
      if (!request.current()) return;
    } finally {
      request.finish();
    }
    const [snapshot, definition] = pair;
    if (runsState.activeRun !== id) return;
    runsState.runDefinition = definition;
    if (readView().mode !== "monitor") setMode("monitor");
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
    const stream = new EventSource(
      `/api/v1/runs/${id}/events?after_seq=${eventCursor}`,
    );
    runsState.stream = stream;
    stream.onopen = () => {
      if (runsState.activeRun === id) {
        $("connection-state").textContent = "已连接";
      }
    };
    stream.onmessage = (event) => {
      if (runsState.activeRun !== id || runsState.stream !== stream) return;
      const data = JSON.parse(event.data);
      if (data.seq <= eventCursor) return;
      eventCursor = data.seq;
      const row = node(
        "li",
        `${data.seq} · ${data.kind} · ${data.node?.name || ""} ${data.node?.state || ""}`,
      );
      row.dataset.node = data.node?.name || "";
      $("timeline").append(row);
      while ($("timeline").children.length > 100)
        $("timeline").firstChild.remove();
      eventFilters();
      if (!runsState.refreshTimer)
        runsState.refreshTimer = setTimeout(() => {
          runsState.refreshTimer = null;
          refreshRun(id).catch((e) => notice(e.message, true));
        }, 150);
    };
    stream.addEventListener("interrupted", (event) => {
      if (runsState.activeRun === id) {
        facts(JSON.parse(event.data));
        closeStream();
        $("stream-state").textContent =
          "原观察者已结束；此服务没有取得停止权限。";
      }
    });
    stream.addEventListener("snapshot", (event) => {
      if (runsState.activeRun === id) facts(JSON.parse(event.data));
    });
    stream.addEventListener("gap", () => {
      // 后续快照只能恢复当前事实，不能清除已经观察到的历史缺口。
      if (runsState.activeRun === id) {
        runsState.gap = true;
        $("stream-state").textContent = "事件存在缺口；快照只能恢复当前事实。";
      }
    });
    stream.addEventListener("final", (event) => {
      if (runsState.activeRun === id) {
        facts(JSON.parse(event.data));
        closeStream();
        $("stream-state").textContent = "终态已保存；快照不补造事件历史。";
        runHistory().catch((e) => notice(e.message, true));
      }
    });
    stream.onerror = () => {
      if (runsState.activeRun === id && runsState.stream) {
        runsState.gap = true;
        $("connection-state").textContent = "连接已断开";
        $("stream-state").textContent =
          "连接中断，等待重连与快照；运行仍由服务持有。";
      }
    };
  }

  function launchErrors() {
    const errors = parameterErrors(),
      timeout = Number($("timeout-ms").value);
    if (!Number.isSafeInteger(timeout) || timeout < 1 || timeout > 3600000)
      errors.push({
        field: $("timeout-ms"),
        message: "任务时限为 1..3600000 毫秒",
      });
    return errors;
  }

  async function start(succeeded) {
    if (dirty() || !readEditor().workflow) throw new Error("先保存版本再运行");
    const targets = selectedTargets();
    if (!targets.length) throw new Error("请选择目标节点");
    clearFieldErrors($("launchpad"));
    $("launch-error").hidden = true;
    const errors = launchErrors();
    if (errors.length) {
      fieldErrors($("launchpad"), $("launch-error"), errors, { focus: true });
      throw new Error("请修正启动参数；尚未提交运行。");
    }
    const timeout = Number($("timeout-ms").value);
    const request = {
      workflow_id: readEditor().workflow.workflow_id,
      revision: readEditor().savedRevision,
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
    runsState.activeRun = "";
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
      succeeded("已创建运行");
      closeDialog("launchpad");
      await openRun(submitted.run_id);
      await runHistory();
    } finally {
      syncButtons();
    }
  }

  async function runHistory(more = false) {
    if (!more) {
      runsState.runOffset = 0;
    }
    const filter = readEditor().workflow
      ? "&workflow_id=" + readEditor().workflow.workflow_id
      : "";
    const list = $("run-history");
    if (list.dataset.scope !== filter) {
      list.dataset.scope = filter;
      list.replaceChildren();
      runsState.runOffset = 0;
      more = false;
      $("more-runs").hidden = true;
    }
    list._retry = () =>
      runHistory(more).catch((error) => notice(error.message, true));
    delete list.dataset.readError;
    const request = beginLoading(list);
    resourceFilters();
    try {
      const data = await api(
        `runs?offset=${runsState.runOffset}&limit=20${filter}`,
      );
      if (!request.current()) return;
      const host = $("run-history");
      const existing = new Map(
        Array.from(host.querySelectorAll(".resource-row"), (row) => [
          row.dataset.record,
          row,
        ]),
      );
      const rows = more ? [...host.querySelectorAll(".resource-row")] : [];
      runsState.runOffset = data.next_offset;
      $("more-runs").hidden = !data.has_more;
      for (const item of data.runs) {
        const id = item.run_id;
        const row = recordRow(
          existing.get(id),
          {
            id,
            kind: "run",
            title:
              (item.target_nodes || []).join("、") || item.run_id.slice(0, 8),
            detail: shortTime(item.started_at),
            fullTime: formatTime(item.started_at),
            search: `${resultLabel(item.task_result)} ${item.reason || ""}`,
            result: item.task_result,
          },
          guard(() => openRun(id)),
        );
        row
          .querySelector(".record-select")
          .setAttribute("aria-current", String(id === runsState.activeRun));
        if (!rows.includes(row)) rows.push(row);
      }
      reconcileChildren(host, rows);
    } catch (error) {
      if (!request.current()) return;
      list.dataset.readError = error.message;
      throw error;
    } finally {
      request.finish();
      resourceFilters();
    }
  }

  function launch(target) {
    if (target && targetNodes.some((n) => n.name === target))
      targetNames = [target];
    renderTargets();
    showDialog("launchpad");
    syncButtons();
  }

  function readRuns() {
    return Object.freeze({ ...runsState });
  }
  let bound = false;
  function bindEvents() {
    if (bound) return;
    bound = true;
    $("launchpad").addEventListener("input", () => {
      const invalid = [
        ...$("launchpad").querySelectorAll('[aria-invalid="true"]'),
      ];
      if (!invalid.length) return;
      fieldErrors(
        $("launchpad"),
        $("launch-error"),
        launchErrors().filter((error) => invalid.includes(error.field)),
        { announce: false },
      );
    });
    $("target-search").addEventListener("input", renderTargets);
    $("fact-filter").addEventListener("change", filterFacts);
    on("return-to-edit", () => {
      closeDialog("launchpad");
      returnToEditing(pendingNodes()[0]?.id);
    });
    on("open-launchpad", () => launch());
    on("node-run", () => launch(selectedNode()?.name));
    const elapsedTimer = setInterval(() => {
      if (readView().mode !== "monitor") return;
      for (const el of $("run-graph").querySelectorAll("[data-start]")) {
        if (!el.dataset.start) continue;
        const start = Date.parse(el.dataset.start),
          end = el.dataset.end ? Date.parse(el.dataset.end) : Date.now();
        if (Number.isFinite(start) && Number.isFinite(end))
          el.textContent = ((end - start) / 1000).toFixed(1) + "s";
      }
    }, 1000);
    window.addEventListener("pagehide", () => clearInterval(elapsedTimer));
    $("more-runs").addEventListener(
      "click",
      guard(() => runHistory(true)),
    );
    $("start-run").addEventListener(
      "click",
      guard(() => withOperation("start-run", "提交中…", start)),
    );
    $("stop-run").addEventListener(
      "click",
      guard(async () => {
        const id = runsState.renderedRun;
        if (!id || id !== runsState.activeRun || $("stop-run").disabled) return;
        try {
          await withOperation("stop-run", "请求停止中…", async (succeeded) => {
            const result = await api(`runs/${id}/stop`, {
              method: "POST",
              body: {},
            });
            if (result.requested) {
              succeeded("已受理");
              notice("停止请求已受理；终态和清理仍待观察。");
            } else {
              notice(
                result.terminal
                  ? "运行已结束；本次没有请求停止。"
                  : "停止未获受理，请查看运行事实。",
              );
            }
            if (runsState.activeRun === id) await refreshRun(id);
          });
        } finally {
          const current = runsState.snapshot;
          $("stop-run").disabled =
            !current ||
            current.run_id !== runsState.activeRun ||
            current.final ||
            current.owned === false ||
            operationPending("stop-run");
        }
      }, true),
    );
    window.addEventListener("pagehide", closeStream);
  }
  return {
    syncTargets,
    selectedTargets,
    launchFeedback,
    runHistory,
    launch,
    openRun,
    read: readRuns,
    bindEvents,
  };
}
