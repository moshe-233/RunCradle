# 架构与核心契约 (ARCHITECTURE)

RunCradle 采用 **Mode A · Wrap** 架构：以固定版本的 **Process Compose `v1.122.0`** 作为唯一执行内核，由内核负责配置加载、依赖等待、健康探测、进程调度与正常进程组停止；RunCradle 专注于**工作流版本管理（CAS）、零 JSON 门槛的 AST/UI 协同编辑、确定性流图工作台渲染、产品服务生命周期与直接子进程退出回执（`_exec`）**。

当前已开放的产品能力见 [支持矩阵](SUPPORT.md)，后续演进与 P0 工作台重构见 [路线与当前进度](ROADMAP.md)。

---

## 1. 核心数据流 (`Input -> Transform -> Output`)

```text
[人 / Agent 编写或通过工作台抽屉编辑 process-compose.yaml + 可选 ui.json]
        │
        ▼
1. 解析、结构化编辑与投影 (internal/workflow + internal/webui)
   ├─ 校验语法、依赖图、参数绑定与安全预算
   ├─ 原位操作 *yaml.Node AST 与 ui.json（保留原文字节、注释、未知字段与精确大整数）
   └─ 计算不可变版本摘要 (Revision) 与有界图视图 IR (WorkflowView + Display Graph IR)
        │
        ▼
2. 版本库持久化 (internal/runs)
   ├─ CAS 校验 expected_revision (冲突返回 409，绝不覆盖源文件)
   └─ 原子写入 .runcradle/workflows/ 与 .runcradle/library/
        │
        ▼
3. 宿主受理与幂等控制 (internal/service.Host)
   ├─ 检查活动并发槽位 (最多 4 个 active runs，满额返回 429 / 退出码 2)
   ├─ 校验 request_id 与 invocation_digest 幂等映射 (.runcradle/requests/)
   └─ 调用 Engine.Start 异步启动运行实例
        │
        ▼
4. 引擎包装与直接回执 (internal/engine/processcompose + internal/nodeexec)
   ├─ Prepare 生成执行副本：注入参数值，将节点命令包装为 `runcradle _exec`
   ├─ 在私有临时目录与私有 UDS 下启动独立 Process Compose 实例
   ├─ _exec 直接 wait 业务子进程，按 incarnation 原子落盘真实退出码与信号，并将真实信号回放给上游
   └─ 采集上游 WS/API 状态与落盘回执，写入 .runcradle/runs/<run_id>/
        │
        ▼
5. 多端一致性呈现 (internal/httpapi + internal/webui + internal/cli)
   └─ 浏览器五层工作台 (HTTP/SSE 运行事实画布) 与 CLI (workflow run/inspect/stop) 共享同一份运行事实
```

---

## 2. 源码目录与模块职责

产品源码位于仓库根目录（Go `>= 1.26.0`，Linux `amd64`、非 root，第三方依赖仅限 [`go.mod`](../go.mod) 中的 `yaml.v3`、`gorilla/websocket` 与 `golang.org/x/sys`）：

| 代码路径 | 核心类型 / 入口函数 | 职责与系统副作用 |
| :--- | :--- | :--- |
| [`cmd/runcradle`](../cmd/runcradle/main.go) | `main()` | 进程总入口：拦截 `_exec` 内部子命令，绑定 `SIGINT`/`SIGTERM` 上下文并交由 `cli.Run` 处理 |
| [`internal/workflow`](../internal/workflow/document.go) | `Document`, `View`, `Load()`, `ParseDocument()`, `Edit()`, `ResolveValues()` *(包含 `ApplyWorkbenchEdit`)* | 解析原生配置与 `ui.json`、AST 保真修改（单字段与结构化节点/环境变量/依赖/`ui.json` 协同编辑）、参数绑定与 ZIP 导出；通过 `os.Root` 严格限制在授权工作区内只读访问 |
| [`internal/runs`](../internal/runs/store.go) | `Store`, `WorkflowRecord`, `ServerRecord` | 管理 `.runcradle/` 私有目录（目录 `0700`、文件 `0600`）：CAS 版本库、`request_id` 映射、运行快照与有界事件流原子读写 |
| [`internal/service`](../internal/service/host.go) | `Host`, `Request`, `Submission`, `StartRun()` | 共用业务用例层；`Host` 持有内存中活动运行的生命周期、并发槽位、幂等判重与终态释放 |
| [`internal/engine`](../internal/engine/engine.go) | `Engine`, `StartRequest`, `Snapshot`, `Event`, `StartRejection` | 极简引擎接缝接口与标准运行事实数据结构，不含任何调度逻辑 |
| [`internal/engine/processcompose`](../internal/engine/processcompose/adapter.go) | `Adapter`, `Prepare()` | Process Compose 适配器：为每次运行分配独立 UDS 与临时目录，驱动上游 API/WS、合并 `_exec` 回执并执行有界停止 |
| [`internal/nodeexec`](../internal/nodeexec/exec.go) | `Main()`, `Receipt`, `ReadReceipts()` | 直接子进程包装器（`_exec`）：登记进程组、等待子进程退出、原子写入回执文件并回放真实信号 |
| [`internal/processcompose`](../internal/processcompose/check.go) | `Probe()`, `Check()` *(P1 扩展 `ResolveBinary`)* | 固定内核版本（`v1.122.0`）探测与隔离环境下的离线加载期 dry-run |
| [`internal/httpapi`](../internal/httpapi/server.go) & [`internal/hostclient`](../internal/hostclient/client.go) | `Open()`, `Server`, `Connect()` | Loopback HTTP/SSE 服务端（含一次性 URL 配对、Cookie/Bearer 鉴权、`Host`/`Origin` 校验、工作流/运行路由及 W3.5 工作区文件只读发现）与 CLI 客户端 |
| [`internal/webui`](../internal/webui/render.go) | `Handler()`, `Render()`, `Display()` | 嵌入式前端资源（HTML/CSS/JS/SVG）、Sugiyama 层次化 DAG 布局与有界图视图 IR，同时驱动在线工作台与无执行权限的离线静态 HTML 导出 |
| [`internal/cli`](../internal/cli/cli.go) & [`internal/output`](../internal/output/sink_linux.go) | `Run()`, `Sink` | CLI 命令分发、退出码映射与带超时保护的有界输出（`<= 4096 Bytes`）；深层函数绝不调用 `os.Exit` |

---

## 3. 引擎接口契约 (`internal/engine/engine.go`)

CLI 阅读入口为 `cli.Run`：`runWorkflow` / `stopWorkflow` 连接显式宿主，`inspectWorkflow` 只读离线历史。运行参数由 `runOptions` 保存；提交身份由 `remoteResult` 持有，提交与不确定响应处理、运行观察、显式取消分别组织。先连接宿主再校验运行来源，保留错误优先级；错误结果保留已知 request/run ID 以便恢复查询。

运行服务从 `Host.StartRun` 顺序阅读：`prepareRun` 一次完成来源绑定、目标规范化及参数解析；`identifyRun` / `replayRun` 使用持久身份判断重放；`admitRun` 登记请求并取得槽位；`launchRun` 移交 `acceptedRun` 给私有 `runExecutor`。请求 DTO 不携带隐藏的执行模式。移交前失败由 Host 释放槽位，移交后由执行器的 defer 释放。`runRecorder` 保存事件与快照，启动/停止观察失败由具名处理函数保留各层事实。终态落盘、活动句柄移除、worker done 关闭是有序但独立的三个步骤。

引擎接缝保持最小化，定义为 [`Engine`](../internal/engine/engine.go#L10-L17)：

```go
type Engine interface {
    Capabilities(context.Context) (Capabilities, error)
    Validate(context.Context, WorkflowSource) (Validation, error)
    Start(context.Context, StartRequest) (RunHandle, error)
    Snapshot(context.Context, RunHandle) (Snapshot, error)
    Subscribe(context.Context, RunHandle) (<-chan Event, error)
    Stop(context.Context, RunHandle) (StopResult, error)
}
```

- **能力声明 (`Capabilities`)**：按特性分别标记 `observable`（可观测）、`editable`（可编辑）、`executable`（可执行）。
- **启动前拒绝 (`StartRejection`)**：仅当配置或环境在启动任何实例前被静态/预检拦截时返回 `*StartRejection`（此时可确认为零残留）；一旦开始创建进程或临时资源，后续异常均按实际观察记录 `cleanup` 状态。
- **配置与运行隔离**：
  - `revision`：标识不可变的原始工作流文件与 `ui.json` 内容。
  - `invocation_digest`：标识规范化调用参数（工作流身份、`revision`、排序后的 `target_nodes`、`timeout_ms`、引擎 SHA、预设与参数值、证据留存开关）。
  - `execution_digest`：标识注入参数并包装 `_exec` 后的实际执行配置副本。

---

## 4. 关键生命周期与安全不变量

### 4.1 显式宿主与停止权限边界
- **单工作区单宿主**：`runcradle serve` 通过 `.runcradle/server/` 下的独占锁管理当前工作区。`workflow run` 与 `stop` 必须连接存活宿主；`validate`、`render` 与 `inspect` 可离线执行。
- **连接与运行解耦**：HTTP 请求结束、关闭浏览器标签页或 CLI 客户端断连（除显式发送 `SIGINT`/`SIGTERM` 触发取消外）均不会取消后台正在执行的任务。
- **幂等提交 (`request_id`)**：相同 `request_id` + 相同 `invocation_digest` 直接返回已有 `run_id` 与状态，绝不重复触发业务副作用；相同 `request_id` 但参数不同返回 `409 Conflict`。网络超时不确定时，调用方应先用原 `request_id` 查询，严禁盲目换新 ID 重跑。
- **停止权限不可凭空重建**：停止权限仅来自当前宿主进程内存中持有的 `RunHandle`。若宿主遭 `SIGKILL` 崩溃，重启后的新宿主只会将旧未完成记录标为 `interrupted` / `unknown`，**绝不根据历史 PID、端口、UDS 路径或节点名称去接管或杀死未知进程**。

### 4.2 直接退出回执与真实信号回放 (`_exec`)
- **为什么需要 `_exec`**：上游引擎在进程快速退出或被信号杀死时，API 状态可能将信号退出折叠为普通退出码或因断流丢失终态。
- **回执与信号回放机制**：每个普通前台节点由 `runcradle _exec` 直接启动并 `wait` 子进程，按 `incarnation` 原子写入真实 `exit_code` 与 `signal`。若子进程因信号 `S` 终止，`_exec` 在落盘回执（记录 `128 + S` 与 `signal = S`）后，通过空环境的 `/bin/sh` 向自身发送相同信号 `S` 退出，从而将真实的 `WaitStatus`（signaled）交还给上游 Process Compose，绝不用普通 `exit 143` 伪造信号退出。

### 4.3 状态采信、事件流与清理范围
- **事实分列，不猜状态**：结果中分别保存节点的 `raw_state`、规范化 `state`、`health`（无 probe 的 `running` 绝不推导为 `ready`）、直接回执 `exit_code`/`signal`、上游观察码 `upstream_node_exit_code` 以及引擎自身的 `engine_exit_code`/`engine_signal`。
- **落盘 SSE 与缺口保留**：上游 WebSocket 不具备持久游标；断线重连拉取快照仅能更新当前状态，已发生的断流必须标记 `history_integrity = gap`（超预算截断标为 `truncated`）。浏览器 SSE 直接读取 `.runcradle/runs/<run_id>/events.jsonl` 落盘事件，不抢占引擎适配器的唯一订阅通道。
- **限定范围的清理证明 (`registered_process_groups`)**：`cleanup = complete` 仅证明本实例登记的原生进程组（核对 Linux boot ID 与 PID namespace）内已无存活非僵尸进程，且私有临时目录已删除。脱离进程组的后代进程或外部资源不在证明范围内；观察不完整时一律报告 `cleanup = unknown` 并附上 `residual_path`。

### 4.4 WebUI 工作台与视图投影契约（W3.5 工作台）
为解决 W3 平铺表单的易用性瓶颈，[W3.5 工作台设计规范](archive/20261027_legacy_workspace/topics/webui-workbench/DESIGN.md) 在不改动上述执行与存储内核的前提下，对视图层与编辑层确立以下四条架构契约：
1. **单主画布与五层渐进式披露（Archify × Dify 范式）**：合并编辑声明图与运行监控图为全视口统一 DAG 画布，外围采用「顶部全局命令栏 + 可折叠左栏 + 右侧非模态节点检查器抽屉 + 底部可折叠证据控制台 + 模态弹窗」的五层空间结构，隐藏次要配置噪音。
2. **节点卡片与五种依赖条件反馈**：
   - 节点卡片严格分离展示 **进程运行状态 (`state`)** 与 **健康探测状态 (`health`)**，并内嵌 `_exec` 真实退出码回执与实时耗时。
   - 连线按 Process Compose 五种原生 `depends_on` 条件渲染胶囊路标，并在运行态实时推导 `idle`（静止）、`waiting`（琥珀色等待）、`satisfied`（柔和绿色满足）与 `blocked`（红色阻断）四态。现行视觉草案要求连线默认静态，轻微动效只用于活动运行的节点状态图标；终态及不再归本宿主持有的历史图静态。
3. **预计算图视图 IR 与可达性聚光灯**：[`webui.Display()`](../internal/webui/display.go) 在服务端预计算每个节点的结构化摘要（`role`、`command_preview`、`env_items`、`dependencies`）以及 `upstream_ids` / `downstream_ids` 可达闭包，前端无需在浏览器内二次解析 YAML 即可实现零延迟上下游链路追踪。
4. **零 JSON 门槛的 AST 与 `ui.json` 协同编辑（n8n 范式）**：通过 `ApplyWorkbenchEdit` 在 `*yaml.Node` AST 与 `ui.json` 上原子执行节点增删改名、环境变量键值表更新、可视化依赖连线以及「一键将环境变量绑定为 `ui.json` 强类型运行参数」，彻底消除手写 JSON 字符串的交互门槛。

现行视觉与交互以 [2026-10-28 设计草案](design/webui-style/DESIGN.md) 为准；主题实现与离线导出共用 `theme.css` / `theme.js`，不依赖外部资源。原生执行与存储契约不随视觉重构改变。

## 随包内核与构建身份

`internal/buildinfo` 通过发行构建注入 Version、Commit 与 Distribution；源码默认为 `source`，目录包为 `bundle`。`internal/processcompose/pinned.json` 统一官方版本、来源和归档/二进制指纹，准备脚本与离线制包脚本读取同一文件。

CLI 仅在 `serve` 打开 HTTP/存储宿主前和 `validate --check-engine` 中调用 `ResolveBinary`，返回真实绝对路径、来源、版本与 SHA。显式路径优先；完整安装从真实程序所在目录定位清单和 `libexec/process-compose`；仅纯源码无随包标记时允许 PATH。任一级错误即停止；随包引擎哈希不符时不执行 Probe。已验证路径继续传给现有适配器，不改变调度、直接回执或运行存储语义。
