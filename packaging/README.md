# RunCradle 安装与使用手册

RunCradle 是本地工作流工作台，支持浏览器编辑、版本保存、CLI 调用与运行历史查询。本手册随 Linux amd64 发行包交付，可以脱离源码仓库使用。

## 安装

从 [GitHub Releases](https://github.com/moshe-233/RunCradle/releases) 下载归档和同名 `.sha256` 文件。以下以 `0.1.0` 为例，在下载目录执行：

```bash
sha256sum -c runcradle-0.1.0-linux-amd64.tar.gz.sha256
tar -xzf runcradle-0.1.0-linux-amd64.tar.gz
cd runcradle-0.1.0-linux-amd64
./runcradle version
```

预期版本输出为 `runcradle 0.1.0`。请保留整个解压目录：

| 文件 | 用途 |
| --- | --- |
| `runcradle` | CLI、HTTP 服务与内嵌 WebUI |
| `libexec/process-compose` | 固定版本 `v1.122.0` 的执行引擎 |
| `bundle.json` | 发行版本、源码提交、`source_dirty`、引擎指纹与分发文件校验清单 |
| `examples/hello.yaml` | 成功与失败自检样例 |
| `LICENSE`、`THIRD_PARTY_NOTICES.md`、`licenses/` | 项目、引擎及依赖的许可证与来源清册 |

运行环境为 **Linux amd64、非 root 用户**。无需安装 Go、Node.js 或系统级 Process Compose，程序运行时不会下载引擎。业务命令使用的 Python、Docker、Shell 脚本等依赖由工作区环境提供。安装目录可以只读；工作区必须允许当前用户读写。

## 首次运行

在发行包目录中复制自检样例并启动宿主：

```bash
mkdir -p "$HOME/runcradle-demo"
cp examples/hello.yaml "$HOME/runcradle-demo/hello.yaml"
./runcradle serve --workspace "$HOME/runcradle-demo"
```

保持该终端运行。浏览器打开启动 JSON 中的 `pairing_url`，完成一次性配对后即可访问工作台；凭据会从 URL 清除并换成 HttpOnly Cookie。此链接包含临时凭据，请勿转发或写入公开日志。

在工作台导入 `hello.yaml`，保存版本，选择 `success` 节点运行。也可以另开终端，进入同一个发行包目录执行：

```bash
./runcradle workflow run \
  --workspace "$HOME/runcradle-demo" \
  --file hello.yaml --target success --request-id hello-success

./runcradle workflow run \
  --workspace "$HOME/runcradle-demo" \
  --file hello.yaml --target failure --request-id hello-failure

./runcradle workflow inspect \
  --workspace "$HOME/runcradle-demo" \
  --request-id hello-failure --node failure
```

| 自检运行 | 节点真实退出码 | `workflow run` 命令退出码 |
| --- | --- | --- |
| `hello-success` | `0` | `0` |
| `hello-failure` | `7` | `1` |

`inspect` 返回 `0` 表示查询成功，任务是否成功请读 JSON 中的运行事实。重复提交相同 request ID 和相同调用内容会返回原运行；再次执行任务请换一个 ID。

在宿主终端按 Ctrl+C 可有序结束本次服务。它会停止自己持有的活动运行，并在 12 秒总预算内收尾；结束后仍可用 `workflow inspect` 离线读取历史。

## 日常使用

以下以 `runcradle` 表示程序路径；未加入 `PATH` 时，请使用 `./runcradle` 或完整路径。

```bash
runcradle help
runcradle serve --workspace /path/to/project

# 离线静态检查；ui.json 为可选参数描述
runcradle workflow validate --workspace /path/to/project \
  --file process-compose.yaml --ui ui.json

# 导出只读 HTML，不执行工作流
runcradle workflow render --workspace /path/to/project \
  --file process-compose.yaml --ui ui.json --out graph.html

# 连接已启动的宿主；目标节点及预设需在配置中存在
runcradle workflow run --workspace /path/to/project \
  --file process-compose.yaml --ui ui.json --target build \
  --preset development --request-id build-001

runcradle workflow inspect --workspace /path/to/project --request-id build-001
runcradle workflow stop --workspace /path/to/project --run-id RUN_ID
```

输入路径相对于 `--workspace` 解析。`validate` 默认只做静态检查；`--check-engine` 会额外调用固定引擎做加载检查，不执行业务命令。HTML 只展示声明，不提供运行按钮。保存工作流只写入 `.runcradle/` 私有库，并通过 `expected_revision` 检查并发修改；冲突返回 `409`，需先核对新版本再保存。

参数优先级为显式 `values` > `preset` > `default`。运行绑定具体 `revision`；工作台后续保存的新版本不会改变正在运行的配置。导出 ZIP 只包含原生配置和可选 `ui.json`，业务脚本及其他工作区文件需要另行携带。

每个工作区最多一个 `serve`。它只监听 `127.0.0.1`，关闭网页或断开观察连接不会停止任务。缺少宿主时 `run`/`stop` 会报错，不自动启动后台服务。同步 `workflow run` 收到 Ctrl+C 会显式请求停止；`--detach` 提交后可关闭调用终端，稍后再查询。

## 脚本与 Agent 接入

产品版本、API 主版本和契约版本分别表示发行、路由与协议身份。`0.1.0` 使用 API `v1`、契约 `1.0.0`；二进制自带 OpenAPI `3.1.1`。

```bash
# 无需宿主
runcradle api describe
runcradle api schema --workspace /path/to/project --out openapi.json

# 需要显式启动 serve
runcradle api status --workspace /path/to/project
runcradle api request --workspace /path/to/project \
  --path '/api/v1/workflows?limit=10' --out workflows-response.json

# 非阻塞提交；request ID 必须由调用方指定
runcradle workflow run --workspace /path/to/project \
  --file process-compose.yaml --target build --request-id adapter-build-001 --detach
runcradle workflow inspect --workspace /path/to/project --request-id adapter-build-001
```

### 文件与请求

CLI JSON 回执上限为 **4096 字节**。`api request` 将完整响应写入 `--out`，回执包含输出路径、字节数、SHA-256、内容类型和 HTTP 状态。支持 `GET`、`POST`、`PUT`；写操作需通过 `--method` 和 `--input` 提供一个 JSON 对象，字段以导出的 OpenAPI 为准。

`--input` 和 `--out` 必须位于工作区内。输出必须是新文件，禁止覆盖既有文件、写入 `.runcradle/` 或 `.git/`、穿过符号链接目录。输出无法预留时，不发送写请求。HTTP 错误的完整响应也会保存；连接失败会移除预留文件。若返回 `output_failed`，应先检查原请求身份与服务端结果。

`--path` 只接受 `/api/v1/` 下的相对 URL，可带分页查询，不接受跨源 URL、路径回退和 fragment。请求不跟随重定向，不使用环境代理，不自动重试写操作。`--timeout-ms` 范围为 `1..6000`，默认 `6000`，它限制客户端请求等待；工作流运行预算另行设置，默认 5 分钟，最大 1 小时。

### 认证与实例身份

`api status` 核验宿主并返回 URL、实例 ID 和凭据文件位置，输出不含 Token；`api request` 自动完成本地认证。直接 HTTP 接入从已授权工作区的 `.runcradle/server/connection.json` 发现连接，验证私有权限、工作区、服务锁和 `/api/v1/health` 实例后使用 Bearer Token。只接受 `http://127.0.0.1:<port>`，每次响应核对 `X-RunCradle-Instance`，宿主重启后重新发现。Token 与配对链接不得放入版本库或普通运行日志。

在线能力入口为 `GET /api/v1/protocol`，Schema 为 `GET /api/v1/openapi.json`，均需认证。浏览器采用一次性配对和 Cookie；服务端接入可以直接使用 Bearer 认证。

### 受理、重试与事件

- `--detach` 的退出 `0`、HTTP `202` 只表示受理。最终结果通过运行快照与节点事实判断。
- 重试同一次调用时保留 request ID 和调用内容。内容改变会返回 `409`；网络中断或客户端超时不能证明任务未执行。`submission_unknown` 需先查询，不能当作已受理或改用新 ID 盲目重试。
- 容量拒绝若已记录为 `not_submitted`，待容量恢复后应明确使用新 ID。调用方应结合 `code`、`accepted` 和 `next_action` 决策。
- `GET /api/v1/runs/{id}/events` 默认返回有限分页；使用 `Accept: text/event-stream` 获取 SSE。CLI 不包装长连接 SSE。
- SSE 恢复使用 `after_seq` 或 `Last-Event-ID`，前者优先。只有带编号的 `message` 推进游标；`snapshot`、`final`、`interrupted`、`gap` 不推进游标。出现缺口后重新核对快照与历史。
- `stop` 只能操作当前宿主持有的活动运行；确认停止请求不等于确认清理完成。

### 退出码与兼容

| CLI 退出码 | 含义 |
| --- | --- |
| `0` | 当前操作成功；提交、查询和业务成功需按命令区分 |
| `1` | 同步运行的目标任务失败；节点真实退出码在结果中 |
| `2` | 配置或参数错误、无宿主、冲突或容量拒绝等 |
| `124` | 超时 |
| `125` | 内部、存储、传输或未知结果错误 |
| `128+N` | 信号取消；如 `130` / `143` |

HTTP 错误包含稳定 `code` 和 `next_action`，不要解析自然语言错误消息来分支。同一 `/api/v1` 保持现有字段语义，允许新增可选响应字段；客户端忽略未知响应字段，发送请求时遵守严格 Schema。破坏性协议变化使用新的 API 主版本。公共接入方式为 CLI/HTTP/SSE，Go `internal` 包不是公共 SDK。

## 引擎选择、升级与回退

引擎选择顺序如下：

1. 显式 `--process-compose PATH`；指定无效路径时直接拒绝，不回退。
2. 可执行文件真实目录中的 `libexec/process-compose`。
3. 只有没有随包标记的源码构建，才可以查找系统 `PATH`。

未显式指定 `--process-compose` 时，发行包缺引擎、清单不匹配或引擎 SHA-256 不符会使启动在初始化工作区和监听网络前失败，退出 `2`。命令软链接可指向完整安装目录内的 `runcradle`，例如在发行包目录执行：

```bash
mkdir -p "$HOME/.local/bin"
ln -s "$PWD/runcradle" "$HOME/.local/bin/runcradle"
```

确保 `$HOME/.local/bin` 位于 `PATH`。命令已存在时，先核对其用途再自行更换链接。

升级时将新版本解压到独立目录，校验归档，停止旧宿主后再切换命令链接并启动。保留旧目录以便回退；回退前确认旧程序支持当前工作区数据格式。不要覆盖正在运行的安装目录。安装包的切换不自动删除工作区历史。

## 支持范围与运行事实

当前支持单文件 Process Compose 配置的受限子集。多文件合并、`extends`、持续会话、自动触发和热更新尚未开放。不支持的执行字段会在运行前拒绝；静态可解析不等于可执行。

任务在当前用户权限下运行，不提供容器沙箱或提权隔离。每次运行使用私有临时目录和 UDS；`cleanup=complete` 仅证明已登记进程组和私有临时目录完成清理，不覆盖自行脱离管理的子进程。

分别读取节点 `state`、`health`、真实 `exit_code`/`signal`、`engine_exit_code`、`history_integrity` 和 `cleanup`。历史出现 `gap` 或 `truncated` 时，不应宣称日志完整。服务节点收到终止信号时，应结合信号回执与任务结果判断，不能只看 `failed`/`stopped` 标签。

发行信息和问题反馈入口：[RunCradle GitHub 仓库](https://github.com/moshe-233/RunCradle)。
