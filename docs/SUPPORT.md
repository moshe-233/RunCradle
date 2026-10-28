# 支持矩阵与系统边界 (SUPPORT)

**本页仅记录当前已落地的产品能力、启动前拦截黑名单与系统硬预算。**
开发进度、P0 工作台重构与实验状态见 [路线与当前进度](ROADMAP.md)，操作命令见 [使用指南](USER_GUIDE.md)，验收依据见 [验收与证据索引](EVIDENCE.md)。

- **唯一执行内核**：固定 **Process Compose `v1.122.0`**（Mode A · Wrap）。
- **历史边界**：已归档的旧版 `RuntimeHealer` 能力不计入本产品。

---

## 1. 产品能力矩阵

W3 是历史人工验收基线；当前 W3.5 工作台已完成实现、自动化回归及用户人工验收。W3.5 在旧提交 `2e11fde` 的自动化验证见 [历史报告](archive/20261027_legacy_workspace/evidence/w35-workbench/WEBUI_WORKBENCH_VALIDATION.md)；重整后前端的双工具链回归见 [实施记录](design/webui-style/IMPLEMENTATION.md)，用户于 2026-10-28 明确确认 W3.5 人工验收通过，见同一记录中的人工验收确认；该确认不改变下表未开放能力的边界。

| 能力 | 当前实现和边界 |
| --- | --- |
| 配置 | 单文件 Process Compose JSON/YAML、依赖校验、未知字段和大整数保留；多文件与 `extends` 未开放。 |
| 工作流库 | 私有版本库、导入预览、CAS 保存、历史版本、复制与导出；保存不回写源文件。 |
| 工作台 | 五层圆角分区、静态条件连线和节点检查器；蓝色/青绿色均可切换浅色/深暗；检查器关闭保留当前会话未应用输入，显式丢弃、应用到草稿和保存版本分别生效；共享 YAML 节点仍需原文编辑。 |
| 运行与观察 | 本地显式宿主、HTTP/SSE、固定内核、直接子进程回执与离线历史；事件缺口、截断及未知保持原样。 |
| 文件与离线视图 | 授权工作区候选文件发现、无执行权限的静态 HTML；接口和预算见下文。 |
| 高级功能 | `session`、多文件、`namespace`、`replicas`、`watch`、定时及热更新尚未开放；Agent 专用工具与第二内核未实现。 |
| 目录发行包 | Linux amd64，固定官方内核、离线制包、随包优先解析、启动前哈希校验、只读安装及并列目录切换；不提供运行时下载或自动更新。验证见 [P1 记录](bundled-engine/VALIDATION.md)。 |
| 迁移 | 旧 W2 记录可离线读取；旧 RuntimeHealer v2 使用 [手工迁移指南](W3_MIGRATION.md)，不自动执行或翻译。 |

---

## 2. 执行支持白名单 vs 启动前拒绝黑名单

静态解析允许保留未知字段供原文编辑，但在进入实际执行（[`Prepare`](../internal/engine/processcompose/config.go)）或显式引擎检查（[`Check`](../internal/processcompose/check.go)）时，严格执行以下准入规则：

### 允许执行的配置子集（Whitelist）
- **结构与命令**：单文件自包含 JSON/YAML，`processes.<name>` 下的 `command`（字符串）或 `entrypoint`（字符串数组）、自定义项目 `shell`（`shell_command` / `shell_argument`）。
- **目录与环境**：位于授权 `workspace` 内的 `working_dir`；字面字符串形式的 `environment`（`KEY=VALUE`）。
- **依赖与探测**：`readiness_probe`、`liveness_probe`；`depends_on` 的五种原生条件（`process_started`、`process_completed`、`process_completed_successfully`、`process_healthy`、`process_log_ready`）。
- **退出与重启策略**：`success_exit_codes`、`availability.restart`（仅限 `no` 或 `exit_on_failure`）、`availability.exit_on_end`、`availability.exit_on_skipped`。

### 启动前直接拒绝的特性（Blacklist）
命中以下任一条件时，在启动任何进程前直接返回 `unsupported`（CLI 退出码 `2`）：
1. **变量与模板展开**：任何键名或字符串值包含 `$` 或 `{{`（包括生成后的辅助执行路径）。
2. **多文件与外部环境加载**：`extends`、`env_file`、`env_files`、`environment_files`、`vars`、`env_cmd`。
3. **非普通前台进程形态**：`is_daemon: true`、`is_tty: true`、`is_interactive: true`、`is_foreground: true`、`is_elevated: true`、`parent_only: true`、`send_keys`。
4. **副本、自动循环与动态触发**：`replicas != 1`、`restart` 设为除 `no`/`exit_on_failure` 以外的值、`watch`、`schedule`。
5. **外部副作用与自定义日志/关停**：`shutdown.command`、`log_location`、`log_configuration`、`mcp`、`mcp_server`。
6. **不安全的环境路径**：宿主继承的 `PATH` 中包含空项或相对路径（每一项必须为绝对目录路径）。

> **路径与环境隔离细节**：可执行文件名（不含 `/`）先按引擎净化环境解析为绝对路径，再应用节点 `environment`；引擎子进程始终在私有临时目录启动（防止 Process Compose 在解析参数前自动读取业务目录下的 `.pc_env`），并隔离 `PC_*` 与 `XDG_CONFIG_*` 环境变量。

---

## 3. 平台要求、资源硬预算与清理边界

### 系统硬预算一览表

| 类别 | 预算项 | 上限 / 阈值 | 超限或违规行为 |
| :--- | :--- | :--- | :--- |
| **输入与解析** | 原生配置文件大小 | `<= 1 MiB` (UTF-8) | 明确报错拒绝（退出码 `2` / HTTP `400`） |
| | `ui.json` 文件大小 | `<= 256 KiB` | 明确报错拒绝 |
| | 显式进程节点数 | `<= 128` 个 | 明确报错拒绝 |
| | AST 深度 / 别名展开 | 深度 `<= 64` 层，访问 `<= 100,000` 次，展开累计 `<= 8 MiB` | 拒绝解析，防止 YAML 炸弹 |
| **存储与列表** | 单工作流版本数 (`revision`) | `<= 4,096` 个 | 达上限后需复制（`copy`）为新工作流 ID |
| | 库列表 / 运行列表目录扫描 | `<= 10,000` 项（分页 `limit` `1..100`，默认 `20`） | 超限明确报错，不静默漏项 |
| | 工作区文件发现扫描 *(W3.5)* | 访问 `<= 2,000` 项，递归深度 `<= 6`，返回 `<= 200` 个路径 | 超限截断并标记 `truncated: true` |
| **网络与输出** | HTTP 请求体 / 响应体 | 请求 `<= 8 MiB`，响应 `<= 32 MiB`（读取超时 5s，请求头 2s） | 超限拒绝 |
| | 静态 HTML 导出文件 | `<= 32 MiB`（权限 `0600`） | 超限拒绝；覆盖已有文件需 `--force` |
| | CLI 结构化输出摘要 | `<= 4,096 Bytes`（写出预算 2s） | 节点/事件详情超限时标记 `details_truncated: true` |
| | SSE 实时事件流 | 全局 `<= 32` 条，单 run `<= 8` 条（单次写超时 2s） | 慢客户端超时断开，重连后拉取快照与落盘事件 |
| **运行与生命周期** | 活动运行并发槽位 | 每宿主最多 `4` 个活动 run（无隐式排队） | 满额返回 `not_submitted`（HTTP `429` / CLI `2`） |
| | 任务运行时限 (`timeout_ms`) | 默认 `300,000 ms`（5 分钟），范围 `1 .. 3,600,000 ms`（1 小时） | 到期触发停止并返回退出码 `124` |
| | 探测与受理预算 | 版本探测 `2s`，引擎 dry-run `5s`，`StartRun` 受理登记 `5s` | 超时终止操作并记录状态 |
| | 宿主收尾预算 (`SIGINT`/`SIGTERM`) | 上游停止观察 `10s`，宿主总收尾 `12s` | 超时按限定范围兜底终止持有的引擎进程 |
| **日志与诊断** | 引擎 stdout/stderr 内存尾部 | 各保留最新 `64 KiB` | 截断旧内容 |
| | 产品日志与事件落盘 | 日志总计 `<= 8 MiB`（单次 API 响应 `<= 2 MiB`）；持久事件每 run `<= 16 MiB`（单条 `<= 16 KiB`） | 超限标记 `truncated`，最终 `Snapshot` 独立保存 |
| | 可选失败诊断 (`--retain-evidence`) | 默认关闭；开启后每节点 `<= 512 Bytes`（去控制字符），保留 `7` 天 | 7 天后在下次读取/保存时惰性清除正文并标为 `expired` |

### 清理范围与安全边界（Cleanup & Security Scope）
- **`cleanup = complete` 的精确定义**：仅代表本实例已登记的原生进程组（`cleanup_scope = registered_process_groups`，经 boot ID 与 PID namespace 核对）已无运行中成员（僵尸进程除外），且本次私有临时目录已成功删除。
- **不承诺沙箱隔离**：业务命令以当前普通用户权限直接执行。若用户脚本自行 `setsid`/脱离进程组、启动后台守护进程或创建外部文件/容器，不在 `cleanup = complete` 的证明范围内；观察不足时如实记录 `cleanup = unknown` 与 `residual_path`，绝不按端口或名字盲目杀进程。
