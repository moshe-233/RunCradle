# RunCradle

**把 Process Compose 原生 JSON/YAML 变成可编辑、可保存、可运行、可复用的确定性 WebUI 工作台与 CLI 工具。**

- **集成模式（Mode A · Wrap）**：以固定版本的 **Process Compose `v1.122.0`** 作为唯一执行内核。RunCradle 负责配置解析、零 JSON 门槛的 AST/UI 保真协同编辑、不可变版本库（CAS）、确定性动态流图工作台渲染与直接子进程退出回执（`_exec`），不重复造调度器。
- **人机同构，零模型依赖**：日常导入、编辑、保存、运行和排障 **100% 无需调用大模型**。人类与 Agent 共用同一套原生配置文件、不可变 `revision` 与本地服务接口。
- **运行环境**：Linux `amd64`、非 root、构建要求 Go `>= 1.26.0`。前端（HTML/CSS/JS/SVG）内嵌于单二进制中，运行时无需 Node.js（Node 仅用于开发期浏览器自动化测试）。

---

## 目录发行包（Linux amd64）

本地可用 `make package VERSION=0.1.0-dev` 生成包含 `runcradle`、`libexec/process-compose`、校验清单、许可证与自检样例的目录归档（默认要求干净工作树，需先 `make prepare` 准备依赖）。完整解压后直接运行：

```bash
/path/to/runcradle-0.1.0-dev-linux-amd64/runcradle serve --workspace /path/to/project
```

无需另装 Process Compose 或传引擎路径；安装损坏会在打开工作区前拒绝启动，不回退到系统引擎。安装、只读目录、命令软链接和并列升级见 [发行包说明](packaging/README.md)。这是一项本地制包能力，尚未发布远端 release。

## 快速上手（3 步跑通闭环）

所有命令均在仓库根目录下执行：

### 1. 构建并准备固定内核

```bash
make build prepare
```

### 2. 启动显式本地宿主（WebUI & API）

```bash
./.work/bin/runcradle serve \
  --workspace examples/w3-http \
  --process-compose "$PWD/.cache/process-compose/v1.122.0/process-compose"
```

1. **浏览器配对**：终端前台会输出一行包含 `pairing_url` 的 JSON。在浏览器打开该链接完成一次性配对（配对后自动擦除 URL 凭据并换取 `HttpOnly` 会话 Cookie）。
2. **导入与首跑**：在页面「导入工作流」填入 `process-compose.yaml` 与 `ui.json` → 预览并保存版本 → 选择目标节点 `check` 点击运行。默认参数下 `check` 节点真实退出码为 `7`（预期失败演示，样例需系统装有 `python3`，详见 [样例说明](examples/w3-http/README.md)）。
3. **修改与复用**：将 `ui.json` 中 `exit_code` 的默认值改为 `0` 并保存新版本（或直接选择 `success` 预设），再次运行即可观察到成功退出 `0`。

### 3. 使用 CLI 连接同一宿主或离线校验

保持 `serve` 在终端一运行，打开终端二执行 CLI 操作：

```bash
# 提交运行（固定 request-id 保证幂等，重复调用仅返回已有运行结果，不重复执行）
./.work/bin/runcradle workflow run \
  --workspace examples/w3-http \
  --file process-compose.yaml \
  --ui ui.json \
  --target check \
  --preset success \
  --request-id first-success

# 查询运行快照与节点详情（宿主关闭后仍可离线读取历史）
./.work/bin/runcradle workflow inspect \
  --workspace examples/w3-http \
  --request-id first-success \
  --node check

# 停止活动运行（仅限当前宿主持有的运行）
./.work/bin/runcradle workflow stop --workspace examples/w3-http --run-id <RUN_ID>
```

**离线静态校验与无权限 HTML 导出**（无需启动 `serve`，不调用内核、不执行业务命令）：

```bash
./.work/bin/runcradle workflow validate \
  --workspace . \
  --file examples/http-contract/process-compose.yaml \
  --ui examples/http-contract/ui.json

mkdir -p ./.work
./.work/bin/runcradle workflow render \
  --workspace . \
  --file examples/http-contract/process-compose.yaml \
  --ui examples/http-contract/ui.json \
  --out ./.work/http-contract.html
```

> 若需调用固定内核做加载期 dry-run，可显式追加 `--check-engine --process-compose <PATH>`。

---

## 核心工程契约（Invariants）

1. **私有库为主存储，源文件零污染**：导入仅读取预览，保存写入 `.runcradle/` 私有库并强制校验 `expected_revision`（冲突返回 `409` 并保留草稿），绝不回写用户源文件。配置导出 ZIP 仅含原生配置与可选 `ui.json`。
2. **显式前台宿主生命周期**：每个工作区由独占锁限定最多一个 `serve` 宿主（仅监听 `127.0.0.1`）。关闭网页或断开 CLI 不会中断运行中的任务；终端发送 `SIGINT`/`SIGTERM` 时，宿主在 12 秒总预算内有序停止自己持有的运行实例。`workflow run` 缺宿主时直接报错退出 `2`，绝不暗启后台守护进程。
3. **事实分层，未知保持未知**：节点真实退出码/信号（由 `_exec` 捕获）、引擎退出码、I/O 状态、历史完整性（`complete`/`gap`/`truncated`）和清理状态分别记录。`cleanup=complete` 严格限定于已登记原生进程组（`registered_process_groups`）与私有临时目录，不承诺沙箱隔离，也绝不按历史 PID 或端口去杀死未知进程。

---

## 文档导航

集成测试与浏览器自动化测试分别位于 `tests/integration/`、`tests/web/`。

- **仓内文档**：[AI 协同工程宪章 (`AGENTS.md`)](AGENTS.md) · [目录发行包说明 (`packaging/README.md`)](packaging/README.md) · [三节点 HTTP 样例 (`examples/w3-http/README.md`)](examples/w3-http/README.md) · [Apache-2.0](LICENSE) · [第三方声明](THIRD_PARTY_NOTICES.md)
- **本地详细技术规范**（统一在本地忽略目录 [docs/](docs/README.md) 维护，不随 Git 入库）：

| 我想要…… | 阅读文档 |
| :--- | :--- |
| 查看本地文档总索引、源码路径映射与历史归档 | [技术文档中心 (`docs/README.md`)](docs/README.md) |
| 查阅核心数据流、模块拓扑、`Engine`/`_exec` 契约与系统硬预算 | [架构与系统边界 (`docs/ARCHITECTURE.md`)](docs/ARCHITECTURE.md) |
| 查阅原生配置白/黑名单、`ui.json` Schema、AST 编辑与 `revision` | [工作流格式规范 (`docs/WORKFLOW_FORMAT.md`)](docs/WORKFLOW_FORMAT.md) |
| 查阅 CLI 参数、退出码、工作台操作、`.runcradle/` 与 `/api/v1` 接口 | [使用指南与接口手册 (`docs/USER_GUIDE.md`)](docs/USER_GUIDE.md) |
| 查阅环境准备、7 套件浏览器测试、双工具链验收与三条代码阅读路径 | [开发与验证指南 (`docs/DEVELOPMENT.md`)](docs/DEVELOPMENT.md) |
| 了解阶段路线（W0–W7）、待验收项、`OBS` 观察项与历史证据索引 | [路线进度与证据索引 (`docs/ROADMAP.md`)](docs/ROADMAP.md) |
