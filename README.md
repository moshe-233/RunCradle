# RunCradle

本检出用于 DSH 插件接入开发，分支为 `feat/dsh-plugin`，跟踪 `origin/feat/dsh-plugin`。源码已同步到远端基线 `143fd17`；当前只完成源码和工作区准备，插件尚未实现。下方保留 RunCradle 独立产品的说明与使用方法。

## 本地接入工作区与文档索引

根 README 统一项目说明、目录说明和文档入口。根 [AGENTS.md](AGENTS.md) 是长期稳定的本地工程契约；阶段、版本、命令可用状态和进度分别在本页与协作文档中维护。

接续顺序：遵守 AGENTS → 阅读 [当前状态](workspace/STATUS.md) 与 [交接卡](workspace/HANDOFF.md) → 阅读 [已确认接入方向](workspace/DSH-INTEGRATION.md)。旧 v1.2 的独立安装与手动启动服务方案已被新的随包交付方向替代，不再直接执行其任务清单。

| 文档 | 职责 | 更新时机 |
| --- | --- | --- |
| [AGENTS.md](AGENTS.md) | 长期授权边界、证据要求、验证纪律、最小变更与工作保护 | 长期规则变化时 |
| [接入方向与准备范围](workspace/DSH-INTEGRATION.md) | 同仓接入、随包二进制、插件管理服务的已确认方向及待设计事项 | 范围或设计决定变化时 |
| [协作约定](workspace/COLLABORATION.md) | 分工、任务流程、决策与方法论来源 | 协作方式变化时 |
| [当前状态](workspace/STATUS.md) | 当前阶段、分支、命令可用状态与下一步 | 工作单元结束时 |
| [交接卡](workspace/HANDOFF.md) | 基础提交、验证结果、文件变化、风险和接续目标 | 交接或暂停时 |
| [验收记录](workspace/ACCEPTANCE.md) | 已执行的检查、退出码、限制与人工确认 | 验证或验收后 |
| [共享开发指南](CONTRIBUTING.md) | 原项目开发环境、验证与文件归属 | 工具链或共享开发规则变化时 |

AGENTS 与 workspace 是本检出的本地材料，已由忽略规则排除，不随源码提交或插件包分发。根 README 中这一节为本地工作区说明，本轮尚未提交；未来发布前应与公共产品说明区分。`packaging/README.md` 和示例 README 是产品使用文档，保留它们的独立用途；本地协作目录不另设 README 索引。

## 接入开发目录

```text
dsh-runcradle/
├── README.md                         # 产品说明与本工作区统一索引
├── AGENTS.md                         # 本地长期契约，Git 忽略
├── CONTRIBUTING.md                   # 共享开发指南
├── cmd/、internal/                   # 原有 Go 核心与内嵌 WebUI
├── integrations/
│   └── dsh/                          # DSH 插件位置，当前仅目录占位
├── tests/                            # 原有 Go 集成与浏览器测试
├── packaging/、scripts/、third_party/ # 原有发行脚本、文档与许可证
├── workspace/                        # 本地协作材料，Git 忽略
│   ├── DSH-INTEGRATION.md
│   ├── COLLABORATION.md
│   ├── STATUS.md
│   ├── HANDOFF.md
│   ├── ACCEPTANCE.md
│   └── archive/                      # v1.0、v1.1、v1.2 原文
├── .work/                            # 本检出的构建与验证产物
└── .cache/                           # 本检出的依赖缓存
```

用户已选择同仓接入并随插件携带完整 RunCradle 运行包，插件通过包内 CLI 调用并管理自己启动的服务。`integrations/dsh/` 目前仅有 `.gitkeep`；源码、测试、Skill 和打包资源的具体布局在实施计划修订时确定。二进制只进入构建和发行产物。

本目录是独立 Git 仓库，不是原项目的 worktree，不共享索引、构建目录或缓存。原项目 `/home/mio/dev/lab/CICD-lab/RunCradle` 继续独立维护 `main`；后续通过远端同步源码，不从原检出直接写入本检出的产物。

## 当前工程命令

Go 核心已有工具链；本轮基础检查使用以下命令，具体结果见验收记录：

```bash
make fmt-check vet check-tree unit-test build package-unit-test
```

完整引擎集成、race 和浏览器检查按下方“从源码构建”与 CONTRIBUTING 执行，未执行的检查不声明通过。DSH 插件尚无 package.json、依赖或脚本，旧计划的 pnpm 命令不可执行。后续建立插件工程时，将实际命令补入本节。

## 本地历史计划归档

| 版本 | 入口 | 定位 |
| --- | --- | --- |
| v1.0 | [原文](workspace/archive/2026-10-31-runcradle-dsh-integration-plan-v1.0.md) | 初始接入设计，仅供追溯 |
| v1.1 | [原文](workspace/archive/2026-10-31-runcradle-dsh-integration-plan-v1.1.md) | 权限、回执及恢复修订，仅供追溯 |
| v1.2 | [原文](workspace/archive/2026-10-31-runcradle-dsh-delivery-plan-v1.2.md) | 轻量协作文档版；后被同仓随包交付方向替代 |

2026-10-31，v1.0 由旧根 archive、v1.1 由旧根计划文件迁入 workspace/archive；v1.2 本次从 workspace 移入 archive。三份原文摘要如下：

```text
e534a1ec2e6f9e1d1ea1347b146f15c98715562b2d471c963907851bcb40d827  workspace/archive/2026-10-31-runcradle-dsh-integration-plan-v1.0.md
fd6d6094af63db028578e09610dbe24cde9c900f0ecf05d422db71380198726d  workspace/archive/2026-10-31-runcradle-dsh-integration-plan-v1.1.md
9788f787326ec329e70806218b76cf39a72a6f02d5e8b46fad7e850e5a1fe7d1  workspace/archive/2026-10-31-runcradle-dsh-delivery-plan-v1.2.md
```

v1.0 更早来自未带版本号的计划文件。冻结稿中的“当前有效”、相对链接和环境快照均为历史记录，导航以根 README 为准。后续正式实施计划另行修订；任务进度、验证事实和人工确认分别维护，不因目录准备完成而勾选插件任务。

## RunCradle 独立产品

**在本地浏览器中编辑、保存和运行 Process Compose 工作流，也能通过 CLI 和 API 完成同样的操作。**

RunCradle 把工作流配置、依赖图、运行参数和执行记录放在一个工作台里。它使用固定版本的 Process Compose `v1.122.0` 执行任务，提供版本保存、运行观察和直接子进程退出回执。日常使用无需调用大模型。

[下载 v0.1.0](https://github.com/moshe-233/RunCradle/releases/tag/v0.1.0) · [发布说明](packaging/RELEASE_NOTES.md) · [安装与使用手册](packaging/README.md) · [HTTP 样例](examples/w3-http/README.md)

## 可以做什么

- **编辑工作流**：导入单个 Process Compose JSON/YAML 文件，在依赖图、节点表单和源码之间切换；用可选的 `ui.json` 定义参数、绑定和预设。
- **保存与复用**：将配置保存为工作区私有库中的不可变版本；运行绑定具体版本，后续编辑不会改变已有运行。保存不覆盖导入的源文件，配置通过 ZIP 显式导出。
- **查看执行结果**：选择目标节点，观察依赖关系、节点状态和事件；分别查看业务进程退出码、引擎退出码、历史完整性与清理结果。宿主退出后仍可查询本地历史。
- **接入脚本和 Agent**：使用有大小上限的 CLI JSON 回执，或通过 `/api/v1` HTTP/JSON 与 SSE 接口接入。二进制自带能力发现和 OpenAPI 文档。

## 安装与首次运行

正式发行包支持 **Linux amd64、普通用户运行**，包含 RunCradle、固定版本的 Process Compose、许可证和自检样例。无需安装 Go、Node.js 或系统级 Process Compose；工作流自身需要的 Python、Docker 等工具仍需自行准备。

从 [v0.1.0 Release](https://github.com/moshe-233/RunCradle/releases/tag/v0.1.0) 下载以下两个文件，放在同一目录：

- `runcradle-0.1.0-linux-amd64.tar.gz`
- `runcradle-0.1.0-linux-amd64.tar.gz.sha256`

校验、解压并启动自检工作区：

```bash
sha256sum -c runcradle-0.1.0-linux-amd64.tar.gz.sha256
tar -xzf runcradle-0.1.0-linux-amd64.tar.gz
cd runcradle-0.1.0-linux-amd64

./runcradle version
mkdir -p "$HOME/runcradle-demo"
cp examples/hello.yaml "$HOME/runcradle-demo/hello.yaml"
./runcradle serve --workspace "$HOME/runcradle-demo"
```

`serve` 在前台运行。用浏览器打开启动 JSON 中的 `pairing_url` 完成一次性配对，然后导入 `hello.yaml`、保存版本、选择 `success` 节点运行。配对链接包含临时凭据，请勿公开。

另开终端，进入同一发行包目录，也可以直接运行：

```bash
./runcradle workflow run \
  --workspace "$HOME/runcradle-demo" \
  --file hello.yaml --target success --request-id hello-success

./runcradle workflow inspect \
  --workspace "$HOME/runcradle-demo" \
  --request-id hello-success --node success
```

`success` 节点的真实退出码为 `0`。将目标换为 `failure`，并使用新的 `--request-id hello-failure`，可验证失败路径：节点退出码为 `7`，`workflow run` 命令退出码为 `1`。同一个 request ID 用于重试同一次调用；想发起新运行，应使用新的 ID。

**保留完整安装目录。** `runcradle` 会校验随包引擎和清单；只拷贝主程序不构成有效安装。软链接安装、离线使用、升级和回退见[发行包手册](packaging/README.md)。

## 常用命令

以下命令假定 `runcradle` 已通过软链接加入 `PATH`；也可以换成二进制的完整路径。

| 目的 | 命令 | 是否需要 `serve` |
| --- | --- | --- |
| 查看命令参数 | `runcradle help` | 否 |
| 静态校验配置 | `runcradle workflow validate --workspace DIR --file FILE` | 否 |
| 导出只读依赖图 | `runcradle workflow render --workspace DIR --file FILE --out graph.html` | 否 |
| 启动工作台 | `runcradle serve --workspace DIR` | 命令本身启动宿主 |
| 运行指定节点 | `runcradle workflow run --workspace DIR --file FILE --target NODE --request-id ID` | 是 |
| 查询运行与历史 | `runcradle workflow inspect --workspace DIR --request-id ID` | 否，也可读取活动运行 |
| 停止活动运行 | `runcradle workflow stop --workspace DIR --run-id RUN_ID` | 是 |

`--file`、`--ui` 和 API 的输入/输出文件路径相对于工作区解析。`validate` 默认只做静态检查；加 `--check-engine` 才调用固定引擎做加载检查。静态校验通过不代表业务命令会成功。导出的 HTML 不具备执行权限。

## 脚本与 Agent 接入

```bash
# 离线查看版本、能力和预算，导出随二进制携带的 OpenAPI 3.1.1
runcradle api describe
runcradle api schema --workspace /path/to/project --out openapi.json

# serve 启动后，检查连接并查询工作流库
runcradle api status --workspace /path/to/project
runcradle api request --workspace /path/to/project \
  --path '/api/v1/workflows?limit=10' --out workflows-response.json

# 提交后立即返回，再用同一个 request ID 查询
runcradle workflow run --workspace /path/to/project \
  --file process-compose.yaml --target build --request-id build-001 --detach
runcradle workflow inspect --workspace /path/to/project --request-id build-001
```

API 主版本为 `v1`，契约版本为 `1.0.0`。JSON 回执最多 4096 字节；`api request` 把完整响应写入工作区中的新文件，支持 `GET`、`POST`、`PUT`。输出文件已存在时会拒绝执行。`--detach` 返回 `0` 只表示宿主已受理，任务结果须继续查询；网络中断也不代表任务未启动。

认证、错误处理、事件恢复和兼容规则见[接入说明](packaging/README.md#脚本与-agent-接入)。RunCradle 提供通用 CLI/HTTP/SSE 接口；Agent 插件、MCP 服务和语言 SDK 需由接入方实现。

## 使用边界

- **工作区与宿主**：每个工作区最多一个显式启动的 `serve`，仅监听 `127.0.0.1`。缺少宿主时，运行命令退出 `2`。关闭网页或断开观察连接不会停止任务；向宿主发送 `SIGINT`/`SIGTERM` 会触发有序收尾，总预算为 12 秒。
- **配置支持**：当前运行入口支持单文件配置的受限子集；多文件合并、`extends`、持续会话、自动触发和配置热更新尚未开放。未知字段的保留与是否可执行是两件事，以校验结果和 `run_supported` 为准。
- **执行权限**：任务使用当前用户权限运行。RunCradle 不提供容器沙箱；`cleanup=complete` 只覆盖已登记进程组与私有临时目录，不承诺回收自行脱离管理的进程。
- **运行事实**：历史可能是 `complete`、`gap` 或 `truncated`。不要只凭节点颜色判断成功，也不要把引擎退出码当作业务进程退出码。已知观察项见[发布说明](packaging/RELEASE_NOTES.md)。

## 从源码构建

需要 Linux amd64、非 root 用户和 Go `>= 1.26.0`。Node.js 只用于开发期浏览器测试；产品构建和运行不依赖 Node.js。

```bash
git clone https://github.com/moshe-233/RunCradle.git
cd RunCradle
make prepare build

./.work/bin/runcradle serve \
  --workspace examples/w3-http \
  --process-compose "$PWD/.cache/process-compose/v1.122.0/process-compose"
```

这个三节点 HTTP 样例需要 `python3`，默认演示节点退出 `7`；选择 `success` 预设可验证成功路径，操作见[样例说明](examples/w3-http/README.md)。源码构建显示开发版本；发行包的版本和源码提交由制包脚本注入。

```bash
# 完整 Go 验证（含真实引擎、race、模块校验与构建）及 7 套浏览器测试
make prepare prepare-web-test
make verify web-test

# 从干净工作树生成并验证离线目录包
make package-unit-test
make package VERSION=0.1.0
RUNCRADLE_BUNDLE_ARCHIVE="$PWD/.work/dist/runcradle-0.1.0-linux-amd64.tar.gz" make package-test
```

构建和测试产物写入 `.work/`，缓存写入 `.cache/`。制包要求依赖已准备完毕，拒绝脏工作树和覆盖已有归档。开发候选包可显式传 `PACKAGE_FLAGS='--allow-dirty --out .work/dist-candidate'`，其清单会标记源码未提交。

## 源码导航

| 目录 | 职责 |
| --- | --- |
| `cmd/runcradle`、`internal/cli`、`internal/hostclient` | 程序入口、CLI 与本地宿主客户端 |
| `internal/workflow`、`internal/runs` | 配置解析、结构化编辑、版本库与运行记录 |
| `internal/service`、`internal/engine` | 运行受理、生命周期与执行结果 |
| `internal/processcompose`、`internal/nodeexec` | 固定引擎适配与业务子进程退出回执 |
| `internal/httpapi`、`internal/protocol`、`internal/webui` | HTTP/SSE 契约、OpenAPI 与内嵌前端 |
| `tests/integration`、`tests/web` | 真实引擎集成测试与浏览器测试 |
| `packaging`、`scripts`、`third_party` | 发行文档、构建脚本与依赖许可证 |
| `integrations/dsh` | DSH 插件的独立代码位置，当前仅预留目录 |

独立检出、开发环境、验证命令和文件归属见 [开发指南](CONTRIBUTING.md)。静态配置预览见 [HTTP 契约样例](examples/http-contract/README.md)。维护者的 `docs/` 与个人 `AGENTS.md` 在本地维护，不随源码分发。

RunCradle 使用 [Apache-2.0](LICENSE) 许可证。随包引擎与依赖的许可证、来源和版权信息见[第三方声明](THIRD_PARTY_NOTICES.md)。
