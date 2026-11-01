# RunCradle 开发指南

本页说明独立检出、构建测试和文件归属。产品使用见 [README](README.md)，发行包使用见[安装手册](packaging/README.md)。开发不依赖维护者本机的 `docs/` 或 Agent 配置。

## 在独立目录中开发

```bash
git clone https://github.com/moshe-233/RunCradle.git RunCradle-dev
cd RunCradle-dev
git switch -c feature/your-change
```

新的 clone 拥有自己的工作树和 Git 索引；默认构建产物、缓存与测试截图也位于该目录内。不要在多个检出之间共用同一个 `RUNCRADLE_WORK_DIR`，否则它们会写入相同的二进制和测试产物。

如果使用自己的 fork，将 clone 地址换成自己的仓库，再按需要添加原项目为 `upstream`。原项目的下载链接与版本说明只有在独立发行时才需要同步调整。创建开发分支不会改变已发布的版本标签。

## 开发环境

支持 Linux amd64、普通用户运行，Go 最低版本为 `1.26.0`。当前 CI 验证 Go `1.26.0` 与 `1.27.1`；具体矩阵见 [verify.yml](.github/workflows/verify.yml)。

本地工具按用途准备：

- 构建与验证：Go、GNU Make、Bash、Git；`go test -race` 需要 C 工具链。
- 引擎准备与打包：Python `>= 3.11`、curl、tar、sha256sum。
- 浏览器测试：Node.js、npm、unzip，以及 Chrome for Testing 所需的系统库。准备脚本固定 Playwright `1.62.1` 和 Chrome `155.0.8059.39`。

```bash
make prepare build
make prepare-web-test
```

准备步骤会下载开发依赖。`make prepare` 按 [pinned.json](internal/processcompose/pinned.json) 的 SHA-256 校验 Process Compose `v1.122.0`。产品运行时不需要 Go、Node.js 或大模型服务，也不会下载引擎。

## 验证改动

所有命令在仓库根目录执行：

```bash
# 静态检查与包内测试
make fmt-check vet check-tree unit-test

# 完整 Go 验证：含真实引擎集成、race、模块校验和构建
make verify

# 全部 7 套浏览器测试
make web-test
```

`make verify` 已包含 `tests/integration`；单独复核真实引擎时可运行 `make integration-test`。浏览器套件依次验证图模型、前端、编辑交互、动效、易用性、宿主生命周期和真实引擎端到端流程。

| 产物 | 默认位置 | 可选覆盖 |
| --- | --- | --- |
| 构建与测试工作目录 | `.work/` | `RUNCRADLE_WORK_DIR` |
| 开发依赖缓存 | `.cache/` | `RUNCRADLE_CACHE_DIR` |
| 前端截图与相关诊断 | `.work/screenshots/` | `RUNCRADLE_SCREENSHOTS` |

截图默认跟随 `RUNCRADLE_WORK_DIR`。同一检出需要保留不同轮次证据时，可指定独立的工作目录或截图目录：

```bash
RUNCRADLE_WORK_DIR="$PWD/.work/review" make web-test
RUNCRADLE_SCREENSHOTS="$PWD/.work/screenshots-review" make web-test
```

已有匹配版本的浏览器工具时，可用 `RUNCRADLE_PLAYWRIGHT_MODULE_DIR` 与 `RUNCRADLE_BROWSER` 指定位置。自动化结果与人工体验验收分别记录。

## 验证发行包

先完成依赖准备和构建验证。从干净提交打包，再测试实际解压后的产物：

```bash
make package-unit-test
make package VERSION=0.1.0-dev
RUNCRADLE_BUNDLE_ARCHIVE="$PWD/.work/dist/runcradle-0.1.0-dev-linux-amd64.tar.gz" make package-test
```

制包阶段使用缓存离线构建，拒绝未提交源码和覆盖已有归档。本地候选包可显式传 `PACKAGE_FLAGS='--allow-dirty --out .work/dist-candidate'`，清单会记录 `source_dirty=true`。正式发行应从干净提交生成，并核对标签、源码身份、归档 SHA-256 和对应 CI。

## 代码与数据边界

- CLI 通过 `internal/hostclient` 连接显式启动的本地宿主；进程执行与依赖调度使用固定的外部 Process Compose。
- `internal/workflow` 负责配置与编辑，`internal/runs` 保存版本和运行记录，`internal/service` 负责受理与生命周期。
- 保存写入工作区 `.runcradle/`，通过 `expected_revision` 防止覆盖并发修改，不回写导入源文件。
- 节点退出码/信号、引擎退出码、历史完整性与清理结果分别记录；`cleanup=complete` 的范围限于已登记进程组及私有临时目录。
- 前端源码位于 `internal/webui/assets/` 并内嵌进二进制，使用原生浏览器 API。脚本和 Agent 接入使用 CLI/HTTP/SSE，`internal` 包不是公共 SDK。

## 文件与文档归属

| 内容 | 维护位置 | 是否随 Git 分发 |
| --- | --- | --- |
| 产品说明与共享开发指南 | `README.md`、`CONTRIBUTING.md` | 是 |
| 安装、发布与示例说明 | `packaging/`、各示例目录的 `README.md` | 是 |
| 源码、测试、构建脚本、依赖许可证 | 对应产品目录 | 是 |
| DSH 接入源码与随包资源 | `integrations/dsh/` | 是；当前仅预留目录 |
| 构建产物、截图、临时计划和验证日志 | `.work/`；依赖缓存用 `.cache/` | 否 |
| 维护者本地技术记录和历史归档 | `docs/` | 否 |
| 本检出的协作状态、决策、验收与历史计划 | `workspace/`，统一从根 README 索引 | 否 |
| 个人 Agent 说明与工具配置 | 根目录 `AGENTS.md`、`.agents/`、`.codex/` | 否 |

目录检查见 [check-tree.sh](scripts/check-tree.sh)，忽略规则见 [.gitignore](.gitignore)。根目录允许共享的 `CONTRIBUTING.md` 和 `CHANGELOG.md`；如需增加其他公开文档目录，应同时调整目录白名单与忽略规则。不要把本地历史档案整批加入源码仓库。

测试样例与许可证可能被构建或测试直接读取，移动前先检查引用。`packaging/README.md` 会成为发行包内的 README，因此其链接必须在包内可用或指向公开地址。取消文件追踪只影响后续提交，不会删除旧分支、历史提交或已发布标签中的副本。
