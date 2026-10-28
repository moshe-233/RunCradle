import { $, node } from "./view.js";

// 运行观察持有自己的 SSE 与刷新定时器，不修改编辑草稿。
export function createRuns({
  runsState,
  api,
  closeDialog,
  dirty,
  drawGraph,
  encodedValues,
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
}) {
  function closeStream() {
  // 只释放页面观察资源；业务任务继续由宿主持有。
    if (runsState.stream) runsState.stream.close();
    runsState.stream = null;
    if (runsState.refreshTimer) clearTimeout(runsState.refreshTimer);
    runsState.refreshTimer = null;
  }

  function facts(snapshot) {
    if (snapshot.run_id !== runsState.activeRun) return;
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
    $("run-engine").textContent =
      snapshot.engine_exit_code === null
        ? `未知${snapshot.engine_signal ? " · signal " + snapshot.engine_signal : ""}`
        : String(snapshot.engine_exit_code);
    $("run-integrity").textContent = runsState.gap
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
      const item = node(
        "div",
        undefined,
        "node-fact" + (n.state === "failed" ? " alert" : ""),
      );
      item.append(
        node("strong", n.name),
        node(
          "div",
          `${n.state} · health ${n.health} · 实际退出 ${n.exit_code === null ? "未知" : n.exit_code}${n.signal ? " · signal " + n.signal : ""} · 来源 ${n.source}`,
        ),
      );
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
    if (runsState.runDefinition)
      drawGraph(
        $("run-graph"),
        runsState.runDefinition.display,
        snapshot.nodes || [],
      );
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
    runsState.renderedRun = "";
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
    runsState.activeRun = id;
    runsState.gap = false;
    runsState.runDefinition = null;
    $("timeline").replaceChildren();
    $("stream-state").textContent = "正在读取所选运行及其版本…";
    const [snapshot, definition] = await Promise.all([
      api(`runs/${id}`),
      api(`runs/${id}/definition`),
    ]);
    if (runsState.activeRun !== id) return;
    runsState.runDefinition = definition;
    setMode("monitor");
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
    runsState.stream = stream;
    stream.onopen = () => {
      if (runsState.activeRun === id) {
        $("connection-state").textContent = "已连接";
      }
    };
    stream.onmessage = (event) => {
      if (runsState.activeRun !== id) return;
      const data = JSON.parse(event.data);
      $("timeline").append(
        node(
          "li",
          `${data.seq} · ${data.kind} · ${data.node?.name || ""} ${data.node?.state || ""}`,
        ),
      );
      while ($("timeline").children.length > 100)
        $("timeline").firstChild.remove();
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

  async function start() {
    if (dirty() || !readEditor().workflow) throw new Error("先保存版本再运行");
    const targets = Array.from($("run-targets").selectedOptions).map(
      (o) => o.value,
    );
    if (!targets.length) throw new Error("请选择目标节点");
    const timeout = Number($("timeout-ms").value);
    if (!Number.isSafeInteger(timeout) || timeout < 1 || timeout > 3600000)
      throw new Error("任务时限为 1..3600000 毫秒");
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
      $("run-history").replaceChildren();
    }
    const filter = readEditor().workflow
      ? "&workflow_id=" + readEditor().workflow.workflow_id
      : "";
    const data = await api(`runs?offset=${runsState.runOffset}&limit=20${filter}`);
    runsState.runOffset = data.next_offset;
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

  function launch(target) {
    if (target) {
      for (const o of $("run-targets").options) o.selected = o.value === target;
    }
    $("launch-note").textContent = dirty()
      ? "请先保存草稿，再运行。"
      : "运行已保存的版本 " + readEditor().savedRevision.slice(0, 12);
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
    $("start-run").addEventListener("click", guard(start));
    $("stop-run").addEventListener(
      "click",
      guard(async () => {
        const id = runsState.renderedRun;
        if (!id || id !== runsState.activeRun || $("stop-run").disabled) return;
        await api(`runs/${id}/stop`, { method: "POST", body: {} });
        notice("停止请求已受理；终态和清理仍待观察。");
        if (runsState.activeRun === id) await refreshRun(id);
      }, true),
    );
    window.addEventListener("pagehide", closeStream);
  }
  return {
    runHistory,
    launch,
    openRun,
    read: readRuns,
    bindEvents,
  };
}
