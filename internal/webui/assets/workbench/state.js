// 状态在入口创建一次；每个模块仅取得自己可修改的分组。
export function createWorkbenchState() {
  return {
    editor: {
      document: null,
      workflow: null,
      expected: "",
      savedRevision: "",
      committed: null,
      workflowOffset: 0,
      files: undefined,
      importPreview: undefined,
    },
    runs: {
      stream: null,
      activeRun: "",
      runOffset: 0,
      gap: false,
      refreshTimer: null,
      renderedRun: undefined,
      runDefinition: undefined,
      snapshot: undefined,
    },
    inspector: {
      selected: "",
      inspectorDrafts: new Map(),
      binding: undefined,
      addUpstream: undefined,
    },
    view: {
      mode: "design",
      direction: "",
      editorBusy: false,
      disabledBefore: [],
    },
  };
}
