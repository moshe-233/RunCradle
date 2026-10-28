# RunCradle 使用指南与接口手册 (USER_GUIDE)

**本页汇总当前已开放的 CLI 命令、浏览器操作流程、退出码映射以及 HTTP/SSE 接口契约。**
工作流与 `ui.json` 语法见 [工作流格式](WORKFLOW_FORMAT.md)，支持特性与系统预算见 [支持矩阵](SUPPORT.md)，历史工作台设计见 [工作台设计规范](archive/20261027_legacy_workspace/topics/webui-workbench/DESIGN.md)。所有命令默认在仓库根目录下执行。

---

## 1. CLI 命令总览

```text
# 1. 在线宿主与任务控制（需启动 serve 宿主）
runcradle serve --workspace DIR [--port PORT] [--process-compose PATH]

runcradle workflow run --workspace DIR
    (--workflow-id ID --revision REV | --file FILE [--ui FILE])
    --target NAME [--target NAME ...]
    [--preset ID] [--values FILE] [--request-id ID]
    [--timeout-ms N] [--retain-evidence]

runcradle workflow stop --workspace DIR --run-id ID

# 2. 离线校验、静态导出与历史查询（无需启动 serve）
runcradle workflow validate --workspace DIR --file FILE [--ui FILE]
    [--check-engine] [--process-compose PATH]

runcradle workflow render --workspace DIR --file FILE [--ui FILE]
    --out FILE [--force]

runcradle workflow inspect --workspace DIR (--run-id ID | --request-id ID)
    [--node NAME_OR_ID | --events-after SEQ | --nodes-after OFFSET] [--limit N]
```

---

## 2. 离线校验 (`validate`) 与静态导出 (`render`)

```bash
# 静态结构校验（默认不调用引擎、不执行业务命令）
.work/bin/runcradle workflow validate \
  --workspace . \
  --file examples/http-contract/process-compose.yaml \
  --ui examples/http-contract/ui.json

# 显式调用固定上游引擎做加载期 dry-run（版本探测 2s，声明检查 5s）
.work/bin/runcradle workflow validate \
  --workspace . \
  --file examples/http-contract/process-compose.yaml \
  --check-engine \
  --process-compose "$PWD/.cache/process-compose/v1.122.0/process-compose"

# 导出离线静态 HTML（权限 0600，上限 32 MiB，覆盖已有文件需加 --force）
mkdir -p .work
.work/bin/runcradle workflow render \
  --workspace . \
  --file examples/http-contract/process-compose.yaml \
  --ui examples/http-contract/ui.json \
  --out .work/http-contract.html
```

- **路径与安全边界**：`--file` 与 `--ui` 相对于 `--workspace` 解析；未传 `--ui` 时不自动寻找同目录文件。`render` 禁止覆盖输入的原生配置或 `ui.json`（含符号链接别名）。
- **离线 HTML 特性**：包含声明依赖图（SVG）、步骤列表、键盘导航、配置原文与只读参数说明；内嵌 CSP `connect-src 'none'`，不含配对 Token，无任何执行权限。支持节点搜索、缩放、主题切换与 `Upstream`/`Downstream` 可达性追踪。
- **`validate` 返回状态**：
  - `Status`：`static_valid` | `invalid` | `validated` | `engine_check_failed`
  - `EngineCheck`：`not_requested` | `passed` | `rejected` | `unavailable` | `unsupported` | `timeout` | `cancelled` | `error`

---

## 3. 启动本地宿主 (`serve`) 与浏览器闭环

```bash
make build prepare
.work/bin/runcradle serve \
  --workspace examples/w3-http \
  --process-compose "$PWD/.cache/process-compose/v1.122.0/process-compose"
```

引擎路径仅在 `serve` 和 `validate --check-engine` 中解析：显式 `--process-compose PATH` → 真实可执行文件所在安装目录的随包内核 → 仅无随包标记的源码构建从 PATH 查找。显式空值或错误不回落；随包缺失、清单身份不匹配、SHA 错误或越界链接直接返回 `2`，在创建 `.runcradle/`、宿主锁和监听端口前失败。内核解析总预算 5 秒，版本探测 2 秒，随包 SHA 在执行版本探测前核对。离线命令不解析内核。

完整目录包无需上述 `--process-compose` 参数，支持任意 cwd、中文/空格路径、外部命令软链接和只读安装目录；只复制主程序属于不完整安装。制包与升级见 [发行包说明](../packaging/README.md)。

1. **一次性安全配对**：
   - `serve` 仅监听 `127.0.0.1`（默认动态端口），每个工作区由 `.runcradle/server/` 独占锁限制单实例。
   - 启动后前台输出一行 JSON：`{ "status": "serving", "url": "...", "instance_id": "...", "pairing_url": "..." }`。
   - 在浏览器打开 `pairing_url` 完成一次性配对（有效期 5 分钟，单次有效），页面立即从地址栏清除 URL 凭据并换取 `HttpOnly`、`SameSite=Strict` 会话 Cookie。后续在同一浏览器可直接访问 `url`。
2. **导入 → 编辑 → 保存 → 运行 → 复用**：
   - 在「导入工作流」选择或填写 `process-compose.yaml` 与可选 `ui.json`，预览并保存版本。
   - 使用工作台画布选择节点，在检查器编辑环境变量、依赖或参数绑定；选择目标节点及预设后运行，在画布和证据区域查看结果。
   - 工作台支持蓝色/青绿色与浅色/深暗独立切换，并在浏览器保存外观偏好；关闭节点检查器会保留当前会话中未应用的输入，“丢弃修改”显式恢复声明，“应用到草稿”和“保存版本”分别生效。高级节点与运行选项默认折叠；证据区分为概览、日志（结构化事件与可选失败线索）和运行详情。
   - 当前源码含 W3.5 工作台；历史自动化验证和待完成人工验收的边界见 [证据索引](EVIDENCE.md)。
   - **版本隔离不变量**：每次运行固定绑定保存时的 `revision`；修改草稿或保存新版本不会重启正在运行的任务，也不会篡改历史运行的视图。

---

## 4. 在线运行 (`run`)、停止 (`stop`) 与宿主生命周期

- **显式连接宿主**：`workflow run` 与 `workflow stop` 自动读取 `.runcradle/server/` 连接当前工作区的 `serve` 宿主（使用私有 Bearer Token，跳过环境代理，拒绝重定向）。若宿主未启动，直接返回退出码 `2` 并提示先运行 `runcradle serve`。
- **两种运行入口**：
  - `--workflow-id ID --revision REV`：运行私有库内已保存的指定版本。
  - `--file FILE [--ui FILE]`：客户端读取并冻结工作区文件提交给宿主运行，不会在私有库中自动创建工作流条目。
- **并发与幂等（`request_id`）**：
  - 每个宿主最多同时运行 **4 个活动任务**（默认超时 `300,000 ms`，范围 `1 .. 3,600,000 ms`），无隐式队列；满额时持久记录 `not_submitted` 并返回退出码 `2`（HTTP `429`）。
  - 相同 `request-id` + 相同规范化调用参数直接返回已有 `run_id`，不产生二次业务执行；相同 `request-id` 但参数不同返回 `409 Conflict`（退出码 `2`）。若提交时网络中断，应先用原 `request-id` 执行 `inspect` 查询，切勿盲目更换 ID 重跑。
- **信号与有界收尾**：
  - CLI `workflow run` 默认阻塞等待终态；收到 `SIGINT`/`SIGTERM` 时向宿主请求停止该次运行并返回 `128 + signal`。
  - `runcradle serve` 收到 `SIGINT`/`SIGTERM` 时停止受理新请求，并向所有持有的运行实例发起停止（上游停止观察预算 **10 秒**，宿主总收尾预算 **12 秒**）。若宿主遭 `SIGKILL` 强杀，重启后的新宿主只将旧记录标为 `interrupted`/`unknown`，绝不按历史 PID 或端口接管残留进程。

### `workflow run` 退出码速查表

| 退出码 | 含义与判定条件 |
| :--- | :--- |
| **`0`** | 所有目标节点成功退出、引擎退出 `0`、I/O 完整，且限定范围清理完成（`cleanup = complete`） |
| **`1`** | 已知目标节点执行失败（节点真实退出码如 `7`、`8` 在 JSON 结果中单独列出） |
| **`2`** | 参数错误、静态/能力校验拒绝、宿主未启动、`request_id` 冲突（`409`）或并发满额（`429`） |
| **`124`** | 任务到达 `timeout_ms` 时限超时 |
| **`125`** | 内部错误、网络/输出交付失败（写出超时 2s），或关键状态/清理观察为 `unknown` |
| **`128 + N`** | 被信号 `N`（如 `SIGINT=2 -> 130`，`SIGTERM=15 -> 143`）显式取消 |

---

## 5. 历史查询 (`inspect`)、分页与诊断留存

`workflow inspect` 为纯只读离线操作（无需 `serve` 存活，不加载当前工作区源文件，不触碰任何进程）：
- **注意**：`inspect` 退出码 `0` 仅代表**历史记录读取成功**，被查询的任务本身可能是失败状态（需查看返回 JSON 中的 `task_result`）。
- **有界输出与分页**：
  - CLI 单次 JSON 输出严格 `<= 4,096 Bytes`，优先展示目标节点；超出字节预算时标记 `details_truncated: true`。
  - 使用 `--nodes-after OFFSET`（配合返回值的 `next_node_offset`）或 `--events-after SEQ`（配合 `next_seq`）分页读取，支持 `--limit 1..100`（默认 `20`）。
  - 使用 `--node NAME_OR_ID` 单独查询指定节点的历史声明与运行事实。
- **可选失败诊断留存 (`--retain-evidence`)**：
  - 默认不持久化保存节点错误日志正文。
  - 运行时显式传入 `--retain-evidence` 后，对失败节点保存最多 `512 Bytes`（剔除终端控制字符）的诊断线索，默认保留 **7 天**；到期后在下一次读取或写入时惰性清除正文并保留 `expired` 标记。

### `.runcradle/` 私有存储目录结构（目录 `0700` / 文件 `0600`）

| 子目录 | 存储内容 |
| :--- | :--- |
| `library/` | 工作流列表与当前 head `revision` 索引（每工作流最多 `4,096` 个版本） |
| `workflows/` | 不可变工作流定义与 `ui.json` 原文快照 |
| `runs/` | 每次运行的元数据、最终 `Snapshot`、调用记录（`invocation.json`）与有界事件流（`<= 16 MiB`） |
| `requests/` | `request_id` 到 `run_id` 及 `invocation_digest` 的幂等映射 |
| `locks/` | 活动运行槽位锁 |
| `server/` | 当前存活宿主的连接描述、私有 Bearer Token 与独占 owner 锁 |

---

## 6. HTTP / SSE 接口契约 (`/api/v1`)

- **安全校验**：所有请求 `Host` 必须精确匹配 `127.0.0.1:<port>`；带 `Origin` 的请求必须同源；使用会话 Cookie 的写请求强制要求携带同源 `Origin`。请求体上限 `8 MiB`（拒绝未知 JSON 字段），响应体上限 `32 MiB`。

| 方法与路径（前缀 `/api/v1`） | 请求参数 / Body | 响应结果与状态码 |
| :--- | :--- | :--- |
| `POST /session` | `{ "token": "..." }`（需同源 `Origin`） | 校验一次性配对 Token，下发 `HttpOnly` 会话 Cookie |
| `GET /health` · `GET /capabilities` | 无 | 返回宿主状态、实例 ID 与引擎能力矩阵 |
| `GET /workspace-files` *(W3.5 新增)* | 无 | 在 `os.Root` 与扫描预算内返回工作区候选 YAML/JSON 与 `ui.json` 相对路径列表 |
| `GET /workflows?offset=&limit=` | 分页查询参数（`limit` `1..100`，默认 `20`） | 返回私有库工作流列表、`next_offset` 与 `has_more` |
| `POST /imports` | `{ "file": "...", "ui"?: "..." }` | 只读加载授权工作区内文件并返回预览视图 |
| `POST /validate` | 草稿对象 + 可选 `edit`（W3 单字段）或 `workbench_edit`（W3.5 零 JSON 结构化节点/`ui.json` 协同操作） | 返回修改后原文、`WorkflowView` 与扩展图 IR 投影、校验问题与 `run_supported` |
| `POST /render` | 草稿对象 | 下载无执行权限的离线静态 HTML |
| `POST /workflows` | 草稿对象 | 在私有库创建新工作流（生成 64 位十六进制随机 ID） |
| `GET /workflows/{id}?revision=` | 可选指定历史 `revision` | 读取工作流当前版本或指定历史版本 |
| `PUT /workflows/{id}` | 草稿对象 + `expected_revision` | CAS 保存新版本；版本不匹配返回 `409 Conflict` |
| `POST /workflows/{id}/copy` | `{ "revision": "..." }` | 基于指定版本复制出新工作流 ID |
| `GET /workflows/{id}/export?revision=` | 可选指定 `revision` | 下载包含原生配置与可选 `ui.json` 的 ZIP 包 |
| `POST /runs` | `{ workflow_id, revision, request_id, target_nodes, timeout_ms?, preset_id?, values?, retain_evidence? }`（或用 `document` 替代 `workflow_id`） | 5s 内登记并返回 `202 Accepted`；冲突返回 `409`；满额返回 `429` |
| `GET /runs?workflow_id=&offset=&limit=` | 可选按 `workflow_id` 过滤及分页 | 返回运行历史列表（纯只读，不加载工作区源文件） |
| `GET /run-requests/{request_id}` | 路径参数 `request_id` | 查询幂等映射对应的 `run_id` 与最新落盘快照 |
| `GET /runs/{id}` · `/definition` · `/invocation` | 路径参数 `id` | 分别返回运行 `Snapshot`、当时的工作流定义、参数调用记录 |
| `POST /runs/{id}/stop` | `{}` 或 `{ "cancellation_code": 130 \| 143 }` | 停止当前宿主持有的活动运行 |
| `GET /runs/{id}/events` | 支持 `?after_seq=` 或 `Last-Event-ID`；`Accept: text/event-stream` 启用 SSE | 分页返回或实时推送落盘事件（全局最多 32 条 SSE 流、单 run 最多 8 条，单次写超时 2s） |
