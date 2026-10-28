import { createWorkbenchState } from "./workbench/state.js";
import { createAPI } from "./workbench/api.js";
import { createEditor } from "./workbench/editor.js";
import { createRuns } from "./workbench/runs.js";
import { createInspector } from "./workbench/inspector.js";
import { createView, $ } from "./workbench/view.js";

// 入口只组装模块；回调在用户操作时调用，避免业务模块循环导入。
const state = createWorkbenchState();
let editor, runs, inspector, view;
const api = createAPI({
  onConnection: (text) => { $("connection-state").textContent = text; },
});
view = createView({
  viewState: state.view,
  addNode: (...args) => inspector.addNode(...args),
  closeInspector: (...args) => inspector.closeInspector(...args),
  inspectorOverlay: (...args) => inspector.inspectorOverlay(...args),
  inspectorPresentation: (...args) => inspector.inspectorPresentation(...args),
  launch: (...args) => runs.launch(...args),
  readEditor: () => editor.read(),
  readInspector: () => inspector.read(),
  readRuns: () => runs.read(),
  rememberInspector: (...args) => inspector.rememberInspector(...args),
  selectWorkbenchNode: (...args) => inspector.selectWorkbenchNode(...args),
  syncButtons: (...args) => editor.syncButtons(...args),
});
inspector = createInspector({
  inspectorState: state.inspector,
  api,
  closeDialog: (...args) => view.closeDialog(...args),
  currentCanvas: (...args) => view.currentCanvas(...args),
  currentDisplay: (...args) => view.currentDisplay(...args),
  draft: (...args) => editor.draft(...args),
  guard: (...args) => view.guard(...args),
  on: (...args) => view.on(...args),
  readEditor: () => editor.read(),
  readRuns: () => runs.read(),
  readView: () => view.read(),
  selectNode: (...args) => editor.selectNode(...args),
  showDialog: (...args) => view.showDialog(...args),
  typedRaw: (...args) => editor.typedRaw(...args),
  updateCanvas: (...args) => view.updateCanvas(...args),
  applyWorkbenchEdit: (...args) => editor.applyWorkbenchEdit(...args),
});
editor = createEditor({
  editorState: state.editor,
  api,
  clearSelection: (...args) => inspector.clearSelection(...args),
  closeDialog: (...args) => view.closeDialog(...args),
  drawGraph: (...args) => view.drawGraph(...args),
  guard: (...args) => view.guard(...args),
  setMode: (...args) => view.setMode(...args),
  notice: (...args) => view.notice(...args),
  on: (...args) => view.on(...args),
  readInspector: () => inspector.read(),
  readRuns: () => runs.read(),
  readView: () => view.read(),
  rememberInspector: (...args) => inspector.rememberInspector(...args),
  resetInspectorDrafts: (...args) => inspector.resetInspectorDrafts(...args),
  runHistory: (...args) => runs.runHistory(...args),
  selectWorkbenchNode: (...args) => inspector.selectWorkbenchNode(...args),
  selectedNode: (...args) => inspector.selectedNode(...args),
  showDialog: (...args) => view.showDialog(...args),
  showDiff: (...args) => view.showDiff(...args),
  sourceLines: (...args) => view.sourceLines(...args),
  updateCanvas: (...args) => view.updateCanvas(...args),
});
runs = createRuns({
  runsState: state.runs,
  api,
  closeDialog: (...args) => view.closeDialog(...args),
  dirty: (...args) => editor.dirty(...args),
  drawGraph: (...args) => view.drawGraph(...args),
  encodedValues: (...args) => editor.encodedValues(...args),
  focusNode: (...args) => view.focusNode(...args),
  guard: (...args) => view.guard(...args),
  setMode: (...args) => view.setMode(...args),
  notice: (...args) => view.notice(...args),
  on: (...args) => view.on(...args),
  readEditor: () => editor.read(),
  readView: () => view.read(),
  renderNodeFacts: (...args) => inspector.renderNodeFacts(...args),
  selectedNode: (...args) => inspector.selectedNode(...args),
  showDialog: (...args) => view.showDialog(...args),
  syncButtons: (...args) => editor.syncButtons(...args),
});

// 所有模块就绪后再绑定事件；每个模块的绑定入口也防止重复初始化。
for (const module of [view, inspector, editor, runs]) module.bindEvents();

view.setEditorBusy(true);
(async () => {
  const fragment = new URLSearchParams(location.hash.slice(1));
  const pair = fragment.get("pair"),
    run = fragment.get("run");
  if (pair) {
    history.replaceState(null, "", location.pathname + location.search);
    await api("session", { method: "POST", body: { token: pair } });
  }
  await editor.library();
  const workflowID = new URLSearchParams(location.search).get("workflow");
  if (workflowID) await editor.openWorkflow(workflowID);
  else await editor.fresh();
  await runs.runHistory();
  if (run && /^[a-f0-9]{32}$/.test(run)) await runs.openRun(run);
  const caps = await api("capabilities");
  $("engine-state").textContent = caps.engine_available
    ? `${caps.engine.engine_kind} ${caps.engine.engine_version}`
    : "引擎未就绪 · 可编辑保存";
})()
  .catch((e) => {
    view.notice(
      `无法连接工作区：${e.message}。首次打开请使用 serve 提供的一次性配对链接。`,
      true,
    );
    $("connection-state").textContent = "未连接";
  })
  .finally(() => view.setEditorBusy(false));
