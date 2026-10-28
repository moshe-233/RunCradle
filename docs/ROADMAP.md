# 路线与当前进度 (ROADMAP)

更新时间：2026-10-28（随包内核实施与前端验证进度）
**本页是当前开发阶段、优先级排序、进行中/暂停专题与下一步行动的唯一总入口。**
产品当前可用边界见 [支持矩阵](SUPPORT.md)，操作说明见 [使用指南](USER_GUIDE.md)，历史测试与验收报告见 [验收与证据索引](EVIDENCE.md)。

> [!IMPORTANT]
> **接手须知（Git 与状态红线）**
> - 进入共享仓库工作前，**必须先执行** `git status --short`、`git branch --show-current` 和 `git log -5 --oneline` 核对现场，严禁盲目 `git reset` 或 `git clean`。
> - 只有真实上游验证与人工验收通过后，才可宣布相关阶段验收完成；本页应分别记录实现、自动化和人工验收状态；局部实验绿灯、测试替身或设计定稿不代表产品能力已开放。

---

## 1. 当前总体结论与优先级决断

- **当前可用产品基线：W3.5（实现、自动化回归与用户人工验收已完成）**
  - 已实现私有工作流版本库（CAS）、AST 表单/原文编辑、`ui.json` 强类型参数与预设、显式 `runcradle serve` 共享宿主、HTTP/SSE 实时事件流、`_exec` 直接退出回执与离线历史查询（详见 [W3 阶段决定](archive/20261027_legacy_workspace/previous_archives/workspace/2026-10-25-docs-consolidation/snapshot/docs/manual-acceptance/2026-10-25-W3_STAGE_DECISION.md)）。
  - 2026-10-28 用户明确确认 W3.5 已完成人工验收，见[人工验收记录](design/webui-style/IMPLEMENTATION.md)。
- **W3 实际试用痛点与 W3.5 插队决断（历史 P0 决策，现已完成）**
  - 在 W3 人工试用中发现：当时 WebUI 将所有参数、源码框与两张静态小图平铺在三栏大表单中，缺乏空间层次与实时状态流光；修改节点的 `environment` 或 `depends_on` 强迫手写 JSON 字符串，增删节点和绑定 `ui.json` 参数更是只能手改源码。
  - **工程优先级判定**：若不在单文件 `task` 基线上先解决界面信息架构与零 JSON 编辑体验，后续叠加 W4 多文件、`session`、`watch` 与热更新只会令界面彻底不可用。因此，正式在 W4 全面铺开前插入 **W3.5：WebUI 立体工作台与零 JSON 交互重构** 作为 **P0 最高优先级专题**（融合 `tt-a1i/archify`、`n8n`、`Dify`、`Argo Workflows`、`Dagster`、`Tilt` 与 `Linear/VS Code` 设计范式，100% 复用现有 Go 执行与存储内核）。
- **遗留待定位观察项（需在后续回归中跟踪，未修复）**
  - **`OBS-01`（历史事件缺口）**：W3 人工验收记录中三次运行的 `history_integrity` 均出现 `gap`。
  - **`OBS-02`（服务节点收尾状态差异）**：被依赖的 `api` 节点在收尾收到 `SIGTERM`（退出码 `143` / signal `15`）时，不同运行间状态在 `failed` 与 `stopped` 之间表现不一致。

---

## 2. 当前排队的实施专题（按 P0 → P2 优先级排序）

三个专题均严格遵循 **Mode A · Wrap**（以固定 Process Compose `v1.122.0` 为唯一加载与执行内核）：

| 优先级与专题名称 | 当前状态 | 首读入口 | 核心范围与下一步行动（Next Steps） |
| :--- | :--- | :--- | :--- |
| **P0（最高优先）**<br>**W3.5 WebUI 立体工作台与零 JSON 交互重构**<br>*(WebUI Workbench)* | **已完成；用户人工验收通过**<br>*(2026-10-28 确认，双工具链回归已记录)* | 1. [工作台设计规范](archive/20261027_legacy_workspace/topics/webui-workbench/DESIGN.md)<br>2. [微步实施计划](archive/20261027_legacy_workspace/topics/webui-workbench/PLAN.md) | **目标**：将平铺大表单重构为「五层立体工作台」：Archify × Argo 全视口 DAG 画布（5 类条件边四态反馈、进程/探针双轨节点卡、上下游聚光灯）、n8n × Dify 右侧零 JSON 检查器抽屉（键值环境变量表、一键绑定 `ui.json` 参数、可视化增删节点与依赖）、免勾选运行启动台弹窗、工作区文件发现与 VS Code 风格双栏 Diff 弹窗。<br>**验收**：用户已确认完成，见[人工验收记录](design/webui-style/IMPLEMENTATION.md)；现行源码回归见 [P1 验证记录](bundled-engine/VALIDATION.md)，前端实施见 [当前计划](design/webui-style/IMPLEMENTATION.md)。历史自动化证据见 [验证报告](archive/20261027_legacy_workspace/evidence/w35-workbench/WEBUI_WORKBENCH_VALIDATION.md)。 |
| **P1（次高优先）**<br>**随包携带 Process Compose 内核**<br>*(Bundled Engine)* | **已实现；本地自动化验收通过**<br>*(双工具链、真实归档；未发布)* | 1. [交付设计规范](archive/20261027_legacy_workspace/topics/bundled-engine/DESIGN.md)<br>2. [当前实施记录](bundled-engine/IMPLEMENTATION.md) | **目标**：生成包含 `runcradle`、`libexec/process-compose` 与 `bundle.json` 的 `tar.gz` 发行包，使 `serve` 免配置开箱即用（与 W3.5 改动模块正交）。<br>**当前**：Task 1—5 已完成；双 Go 工具链完整回归、真实归档 `B-01`—`B-10` 和终审修正已通过，见[验证记录](bundled-engine/VALIDATION.md)。**下一步**：候选包本机试用；远端提交/CI/发布另行执行。W3.5 人工验收已于 2026-10-28 获用户确认。 |
| **P2（暂停候命）**<br>**W4a 同实例多文件实验**<br>*(W4a.0-A Same-Instance)* | **已暂停**<br>*(Task 1/2 完成，Task 3 部分完成，W4a.1 未开始)* | 1. [暂停交接记录](archive/20261027_legacy_workspace/eng-workspace/docs/active/w4a-same-instance/HANDOFF.md)<br>2. [重设计规范](archive/20261027_legacy_workspace/topics/w4a-same-instance/DESIGN.md)<br>3. [实验任务计划](archive/20261027_legacy_workspace/topics/w4a-same-instance/PLAN.md)<br>4. [检查点 JSON](archive/20261027_legacy_workspace/eng-workspace/docs/active/w4a-same-instance/CHECKPOINT.json) | **背景**：旧 W4a.0 方案（从 `/processes` 重建单文件）因丢失项目级全局 `environment` 已淘汰（见 [失败报告](archive/20261027_legacy_workspace/previous_archives/source/2026-10-25-docs-consolidation/snapshot/docs/W4A0_VALIDATION.md)）。新路线 A 在同一原生实例内通过 API 注入 `_exec` 回执并单次调用 `/namespace/start`。<br>**下一步**：待 P0/P1 完成或主人显式指令恢复。恢复时**不要重做 Task 1/2**；先将 Task 3 已通过的 `scenarios.py` 接入 `probe.py` 持久证据，再完成 Task 4 故障注入与 Task 5 最终审查，通过后接入 W3.5 新工作台。 |

---

## 3. 全阶段路线图（W0 — W7）

| 阶段 | 核心交付目标 | 验收出口要求 | 当前状态 |
| :--- | :--- | :--- | :--- |
| **W0 / W0.1** | 产品重定义与独立源码仓 | 旧 `runtime-healer` 归档至 `reference/`，独立建立 `runcradle-source/` 并通过双 Go 工具链构建 | **已完成**（历史计划已归档） |
| **W1** | 原生定义解析与静态视图 | 人/Agent 共用解析器；单入口 JSON/YAML + `ui.json` 校验；保留未知字段；导出无执行权限的静态 HTML | **已交付**（证据已冻结） |
| **W2** | 内核接入与真实运行事实 | 独立 CLI 运行多节点流程；上游执行五种依赖与就绪探测；`_exec` 捕获真实退出码 `0`/`7`/`8` 与信号；离线 `inspect` | **已交付**（直接 `run` 已在 W3 升级为连接 `serve` 宿主） |
| **W3** | 浏览器编辑 → 保存 → 运行 → 复用闭环 | 零模型调用走通 WebUI 导入、AST 表单/原文编辑、CAS 保存、运行、历史查询与再运行；提供旧 v2 手工迁移表 | **已交付**（完成首轮功能验收，暴露出平铺表单易用性瓶颈） |
| **W3.5** | **立体工作台与零 JSON 交互重构** | 融合 Archify/Argo/n8n/Dify/Tilt 范式：五层工作台、全视口静态条件连线画布、右侧零 JSON 节点抽屉（键值表+一键绑定 `ui.json` 参数+节点增删）、免勾选运行启动台、工作区文件发现与双栏 Diff 弹窗 | **已完成；用户人工验收通过**（2026-10-28 确认，现行源码双工具链回归通过） |
| **W4a** | **配置复用**：多文件合并、`extends`、环境/模板与预设 | W4a.0-A 证明同实例桥接与原生加载/执行语义一致；W4a.1 在新工作台打通多文件包保存、编辑与运行；W4a.2 另行设计变量/环境文件 | **进行中/暂停**（同实例实验暂停于 Task 3；产品能力未开放） |
| **W4b** | **持续开发**：`session` 模式、`namespace`、`replicas` | 明确会话寿命（候选默认 8h / 上限 24h）；关闭网页不误停；停止归属清晰；处理副本身份与端口冲突 | **未开始** |
| **W4c** | **自动触发**：`watch` 级联重启、`cron` / `interval` 定时 | 上游负责调度；按 `incarnation` 区分同名节点多次启动；重启不误报整体完成；停止后不再创建新进程 | **未开始** |
| **W4d** | **动态修改**：原生项目/进程配置热更新 | 预览受影响节点集合；用户显式确认应用（保存草稿不等于热更新）；部分失败逐项反馈 | **未开始** |
| **W5** | **Agent 协作**：可选 Agent 工具与 Skill | 人与 Agent 共用同一 `revision` 与服务接口；真实验证双作者并发修改（`409` 冲突保护）与人工接手 | **未开始**（依赖 W3.5，可与 W4 并行小步推进） |
| **W6** | **真实项目狗粮与交付加固** | 至少 1 个真实维护项目、2 类工作流、3 个开发会话、6 次人工复用；对照原生 Process Compose 与 Shell 记录准备/维护成本 | **未开始**（P1 随包内核为提前拆出的交付子专题） |
| **W7** | **内核演进立项准备**（路线终点） | 产出：① 附最小复现的能力缺口清单；② 稳定 `Engine` 契约；③ 兼容测试语料；④ 可移植子集与回滚方案；⑤ 开源替换与最小自研学习原型两份立项提纲 | **未开始**（仅做立项准备，**不**在此路线内开发或切换第二内核） |

---

## 4. 能力开放的停止条件（Stop Conditions）

在任何阶段的推进中，一旦出现以下任一情况，**必须立即阻断对应产品能力的开放**并保留反例证据：
1. 快速退出的任务无法稳定、可信地拿到真实终态退出码。
2. 适配器包装改变了 Process Compose 原生的命令、工作目录、环境或依赖调度语义。
3. 停止逻辑误杀了非本实例持有的未知进程（如按端口或节点名盲目杀进程）。
4. 编辑或 API 往返静默丢弃了用户的未知字段、注释或大整数精度。
5. 将事件流断线缺口（`gap`）或截断（`truncated`）伪造为完整历史，或改写已冻结的历史验证证据。

## 2026-10-28 前端实施补充

用户已要求开始前端重构；现行视觉与交互以 [设计草案](design/webui-style/DESIGN.md) 为准，工程进度与当前源码验证见 [实施计划](design/webui-style/IMPLEMENTATION.md)。蓝色/青绿色均保留明暗模式，连线默认静态，检查器关闭保留未应用输入。该前端轮次未推进 P1/P2；本次在 `codex/bundled-engine` 承接 P1，P2 仍暂停。W3.5 人工验收由用户于 2026-10-28 明确确认完成，见[记录](design/webui-style/IMPLEMENTATION.md)；此状态依据用户确认单独更新。
