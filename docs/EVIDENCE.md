# 验收矩阵与证据索引 (EVIDENCE)

**本页汇总各阶段验证报告入口与全周期验收标准矩阵（WF / RUN / UI / MIG / WB / PC / AG / USE / ENG）。**
当前开发进度、P0 工作台重构与阶段开放决定见 [路线与当前进度](ROADMAP.md)，构建与测试命令见 [开发指南](DEVELOPMENT.md)。

---

## 当前新增验证

**W3.5 用户人工验收已完成**：用户于 2026-10-28 在当前对话明确确认，结论与原话见[人工验收记录](design/webui-style/IMPLEMENTATION.md)。未补写用户未提供的逐项场景或验收提交号。

P1 随包内核的本地归档验收、双工具链结果和候选包身份见 [验证记录](bundled-engine/VALIDATION.md)，实施进度见 [实施记录](bundled-engine/IMPLEMENTATION.md)。当前验证独立于下列历史报告，不改写其字节。

## 1. 已归档阶段验证报告与结构化证据索引

历史验证报告与证据 JSON 均已冻结（保留当时精确的源码 Commit、二进制 SHA-256 与测试输出，不随文档整理改写）：

| 阶段 / 专题 | 验证报告与代码审查原文 | 结构化证据 JSON / 检查点 | 备注与覆盖边界 |
| :--- | :--- | :--- | :--- |
| **W1（静态解析与渲染）** | [W1_VALIDATION.md](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W1_VALIDATION.md) · [W1_REVIEW.md](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W1_REVIEW.md) | [W1_EVIDENCE.json](archive/20261027_legacy_workspace/evidence/w1/W1_EVIDENCE.json) | 覆盖单文件解析、未知字段保留与离线 HTML 导出 |
| **W2（内核接入与回执）** | [W2_VALIDATION.md](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W2_VALIDATION.md) · [W2_REVIEW.md](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W2_REVIEW.md) | [W2_EVIDENCE.json](archive/20261027_legacy_workspace/evidence/w2/W2_EVIDENCE.json) | 覆盖五种依赖、`_exec` 直接回执、信号回放与限定清理 |
| **W3（WebUI 闭环与共享宿主）** | [W3_VALIDATION.md](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W3_VALIDATION.md) · [W3_REVIEW.md](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W3_REVIEW.md) | [W3_EVIDENCE.json](archive/20261027_legacy_workspace/evidence/w3/W3_EVIDENCE.json) | 覆盖私有库 CAS、双模编辑、参数绑定、HTTP/SSE 与 Playwright 回归 |
| **W3 远端 CI** | 对应精确提交 `6a21b402` 的远端流水线记录 | [WORKFLOW_W3_REMOTE_CI.json](archive/20261027_legacy_workspace/evidence/w3/WORKFLOW_W3_REMOTE_CI.json) | 仅证明 `6a21b402` 提交，不自动代表后续分支修改 |
| **W3 人工验收决定** | [2026-10-25-W3_STAGE_DECISION.md](archive/20261027_legacy_workspace/previous_archives/workspace/2026-10-25-docs-consolidation/snapshot/docs/manual-acceptance/2026-10-25-W3_STAGE_DECISION.md) | 原始现场位于本机忽略目录 `.work/manual-acceptance/` | 跑通有指导 HTTP 主线；基于试用痛点立项 P0 **W3.5 立体工作台重构**；保留 `OBS-01`（gap）与 `OBS-02`（收尾状态差异）待查 |
| **W3.5 工作台自动化验证** | [WEBUI_WORKBENCH_VALIDATION.md](archive/20261027_legacy_workspace/evidence/w35-workbench/WEBUI_WORKBENCH_VALIDATION.md) | [WEBUI_WORKBENCH_EVIDENCE.json](archive/20261027_legacy_workspace/evidence/w35-workbench/WEBUI_WORKBENCH_EVIDENCE.json) | 2026-10-26 在旧提交 `2e11fde` 上双 Go 工具链自动化验证；当时 W3.5 人工验收未执行；后续当前回归与 2026-10-28 用户人工验收确认分别见本页当前记录 |
| **W4A0（旧进程 API 投影方案）** | [W4A0_VALIDATION.md](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W4A0_VALIDATION.md) · [W4A0_REVIEW.md](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W4A0_REVIEW.md) | [W4A0_EVIDENCE.json](archive/20261027_legacy_workspace/evidence/w4a0/W4A0_EVIDENCE.json) | **候选失败**：证实 `/processes` 投影丢失项目全局 `environment`，方案已淘汰 |
| **W4a 同实例实验（路线 A）** | [W4A_SAME_INSTANCE_HANDOFF.md](archive/20261027_legacy_workspace/eng-workspace/docs/active/w4a-same-instance/HANDOFF.md) | [W4A_SAME_INSTANCE_CHECKPOINT.json](archive/20261027_legacy_workspace/eng-workspace/docs/active/w4a-same-instance/CHECKPOINT.json) | **暂停于 Task 3（P2）**：smoke `38/38` 通过，但 `gate_passed = false`，尚无完整门槛报告 |

> **历史归档导航**：
> - [内层源码文档归档清单](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/README.md)（含 W0.1 [工程起点验证](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/VALIDATION.md) 与 W1–W3 实施计划原文）
> - [外层工作区文档归档清单](archive/20261027_legacy_workspace/previous_archives/workspace/2026-10-25-docs-consolidation/README.md)（含早期产品定义、架构设计与 [人工验收步骤模板](archive/20261027_legacy_workspace/previous_archives/workspace/2026-10-25-docs-consolidation/snapshot/docs/MANUAL_ACCEPTANCE.md)）
> - [旧版 CLI 基线归档](archive/20261027_legacy_workspace/previous_archives/workspace/2026-10-22-cli-baseline/README.md) · [旧版实验索引](archive/20261027_legacy_workspace/previous_archives/workspace/2026-10-25-docs-consolidation/snapshot/docs/EXPERIMENTS.md)

---

## 2. 全周期验收标准矩阵（Acceptance Matrix）

每项能力必须通过对应验证层的真实断言才算达标。下表是当前验收目标；历史 W3.5 报告中的 WB 编号与本表定义不完全一致，应以报告中的场景描述核对，不能仅按编号对应：

| 编号 (阶段) | 测试输入与触发动作 | 必须观察到的精确结果 | 主要验证层 |
| :--- | :--- | :--- | :--- |
| **WF-01** (W1) | 人类手写与 Agent 生成的语义等价 JSON / YAML | 解析出相同的节点、依赖边与条件；作者身份不影响解析结果 | 单元测试 + 文件夹具 |
| **WF-02** (W1) | 无 `ui.json` / 有效 `ui.json` / 含未知字段 / 含 YAML 共享节点 | 无 `ui.json` 可生成基础页面；未知字段原样保留；共享节点标记需原文编辑 | 单元测试 + 浏览器 |
| **WF-03** (W1) | 缺失依赖、循环依赖、无 probe 的 `process_healthy`、超体积/超深输入 | 精确定位配置路径或行列报错；不启动任何进程；导入与渲染零执行副作用 | 单元测试 + 真实内核 dry-run |
| **WF-04** (W1) | 含 HTML 标签、SVG 脚本、终端 ANSI 控制序列的恶意文本 | 严格按上下文转义为纯文本展示，浏览器中零脚本执行（XSS 防护） | 单元测试 + 真实浏览器 |
| **RUN-01** (W2) | 多节点正常退出 `0` 与业务真实退出 `7` / `8` | 准确捕获每个目标节点真实退出码，引擎退出码单独列出；绝不把业务失败报为成功 | 真实固定内核 |
| **RUN-02** (W2) | 毫秒级快速退出任务、服务就绪前早退、上游异常丢失终态 | 稳定拿到 `_exec` 真实退出码或明确标记 `unknown` | 真实固定内核 + 故障注入 |
| **RUN-03** (W2) | `started` / `completed` / `completed_successfully` / `healthy` / `log_ready` 五种依赖 | 前置条件真实满足后才启动下游节点；前置失败时下游标记为 skipped | 真实固定内核 |
| **RUN-04** (W2) | 运行超时 (`timeout_ms`)、`SIGINT`/`SIGTERM`、忽略 `TERM` 的进程、慢消费者 | 在预算时间内有界返回；停止由上游执行；退出码、I/O 与清理状态分别记录 | 真实固定内核 + CLI |
| **RUN-05** (W2) | 外部未知进程占用端口、多 run 并发、PID / namespace 变化 | 绝不误杀未知占用者；并发 run 之间 UDS、回执与事件完全隔离 | 真实固定内核 + 进程观察 |
| **RUN-06** (W2) | 上游 WS 断线、SSE 慢客户端断开、重复状态推送、节点重启 | 重连后拉取快照更新当前状态；断流显式标记 `gap`；按 `incarnation` 区分重启；不伪造中间事件 | 协议夹具 + 真实固定内核 |
| **RUN-07** (W2) | 日志/事件超出字节预算、未开启 `--retain-evidence` 或已满 7 天 | 显式标记 `truncated`；状态不因截断误报成功；未开启或过期后磁盘无残留诊断正文 | 单元测试 + 目录扫描 |
| **UI-01** (W3) | 浏览器完成：导入 → 编辑 → 保存 → 运行 → 查看历史 → 再次运行 | 零模型调用走通闭环；查看历史不触发重跑；每次运行严格绑定当时的 `revision` | 真实浏览器 + 真实固定内核 |
| **UI-02** (W3) | 双端并发保存同一工作流、选择历史旧 `revision` 运行 | `expected_revision` 不匹配返回 `409` 且不丢失草稿；新版本视图不覆盖历史运行 | HTTP API + 真实浏览器 |
| **UI-03** (W3) | 重复提交相同 `request_id`、提交超时后重查、同 ID 不同参数 | 相同调用返回同一 `run_id` 且无二次副作用；不同参数返回 `409 Conflict` | HTTP API + 真实固定内核 |
| **UI-04** (W3) | 跨 `Origin` 请求、越界读取非工作区目录、请求体注入任意 PID | 全部拦截拒绝；既有版本库与运行记录完好无损 | HTTP 集成测试 |
| **MIG-01** (W3) | 导入旧 v2 策略、离线 `inspect` 旧 W2 记录 | 旧 v2 返回明确迁移提示；旧 W2 记录可离线只读查询且原文件字节零修改 | 兼容性集成测试 |
| **WB-01** (W3.5) | 打开五层工作台或离线 HTML，测试缩放、小地图、蓝色/青绿色和浅色/深暗主题、`Upstream`/`Downstream` 追踪与 `/` 搜索 | 全视口单主画布清晰渲染中性节点卡片与 5 类条件标签边；上下游聚光灯与全局搜索零延迟聚焦目标节点 | 真实浏览器 + 静态 HTML 断言 |
| **WB-02** (W3.5) | 在右侧抽屉执行零 JSON 操作：新建/重命名/删除节点、表格编辑环境变量、一键绑定 `ui.json` 参数、下拉增删依赖 | 全程不写任何 JSON/YAML 字符串即可完成编排并通过校验；底层注释、未知字段与 `int64` 精度完整保留 | 单元测试 + 真实浏览器 |
| **WB-03** (W3.5) | 打开「运行启动台弹窗」（或点击节点 `▶ 运行到此节点`），免勾选修改参数运行，观察条件状态与底部证据控制台 | 参数直接可改；运行态静态连线呈现 `waiting` 等待 → `satisfied` 满足或 `blocked` 阻断；失败节点置顶显示 Alert 诊断卡 | 真实浏览器 + 真实固定内核 |
| **WB-04** (W3.5) | 打开「导入工作流弹窗」检索工作区文件；触发 `409` 冲突或重新导入对比 | `/api/v1/workspace-files` 安全列出工作区候选文件（屏蔽 `.runcradle/` 与越界软链接）；冲突与重导入弹出双栏高亮 Diff 视图 | HTTP 集成测试 + 真实浏览器 |
| **PC-01** (W4a) | 多文件合并、`extends`、项目级/节点级环境与预设 | 同实例执行结果与原生直接执行一致；引用闭包路径安全；参数绑定不拼接 Shell | 真实固定内核对照 |
| **PC-02** (W4b) | `session` 模式、`namespace`、`replicas`、端口冲突 | 会话生命周期明确；关闭网页不误停；仅停止本宿主持有的实例 | 真实浏览器 + 真实固定内核 |
| **PC-03** (W4c) | `watch` 级联重启、`cron`/`interval` 定时触发、停止期间触发到期 | 由上游调度；单次重启不误报流程结束；停止后不再拉起新进程 | 真实固定内核集成 |
| **PC-04** (W4d) | 动态增删改节点并应用、部分节点更新失败 | 应用前展示受影响节点集合；保存草稿不触发热更新；部分失败逐项反馈 | 真实浏览器 + 真实固定内核 |
| **AG-01** (W5) | Agent 起草/校验/修改配置，人类在浏览器审阅并接手运行 | 人机共用同一 `workflow_id` 与 `revision`；留存真实工具调用记录 | 真实 Agent + 人工协作 |
| **USE-01** (W6) | 在真实维护项目中跨开发会话反复使用 | 如实记录首次配置耗时、故障定位步骤、复用次数与维护成本 | 真实项目狗粮记录 |
| **ENG-01** (W7) | 能力缺口盘点与兼容测试语料审核 | 区分上游内核限制与适配器限制；形成开源替换与最小自研原型两份立项提纲 | 架构与契约审查 |

---

## 3. 新增验证证据的记录规范

后续每完成一个阶段或专题验收，新增的结构化证据必须包含以下字段：
- **身份信息**：`stage`、`source_commit`（或 `dirty_manifest`）、`engine_version`、`engine_sha256`、`revision`、`invocation_digest`、`run_id`。
- **执行与观察事实**：实际执行的命令行、耗时、人工操作步骤、节点退出码、`task_result`、`cleanup`（及 `cleanup_scope`）、`history_integrity` 与已知限制说明。
- **卫生原则**：默认不将完整业务日志、会话凭据或宿主完整环境变量提交进 Git 仓库。

2026-10-28 前端重构的当前执行记录见 [工作台实施计划](design/webui-style/IMPLEMENTATION.md)。本地回归与截图不替代远端 CI 或人工验收；用户已于 2026-10-28 另行确认 W3.5 人工验收通过，远端 CI 仍单独记录。
