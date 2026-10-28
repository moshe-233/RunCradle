# 开发与验证指南 (DEVELOPMENT)

**本页说明源码构建、测试依赖准备、双 Go 工具链验证流程以及文档维护规范。**
当前开发任务与路线状态只在 [路线与当前进度](ROADMAP.md) 维护。

- **建议阅读顺序**：[仓库 README](../README.md) → 本页 → 与具体任务相关的 [架构与契约](ARCHITECTURE.md) 或 [工作流格式](WORKFLOW_FORMAT.md)。
- **进入仓库首步检查**：当前仓库根目录是源码与文档的共同入口。动手前先执行以下命令确认工作树现场，**切勿依据旧文档分支名盲目执行 `git reset` 或 `git clean`**：
  ```bash
  git status --short
  git branch --show-current
  git log -5 --oneline
  ```

---

## 1. 显式准备与常规测试

在仓库根目录执行。默认缓存和构建产物分别位于被忽略的 `.cache/` 与 `.work/`；可通过 `RUNCRADLE_CACHE_DIR`、`RUNCRADLE_WORK_DIR` 覆盖。

```bash
# 1. 下载并校验固定版本的 Process Compose v1.122.0（存入 $RUNCRADLE_CACHE_DIR/process-compose/）
make prepare

# 2. 准备浏览器端到端测试依赖（Playwright 1.62.1 与 Chrome for Testing 155.0.8059.39，存入 $RUNCRADLE_CACHE_DIR/web-test/）
make prepare-web-test

# 3. 运行源码树白名单检查、静态/单元/集成验证、上游实验验证与浏览器测试
make verify experiment-test web-test
```

### 复用本机已安装的浏览器测试工具
若本机已有匹配版本的 Playwright 模块与 Chrome for Testing，可通过环境变量显式指定以跳过下载：

```bash
export RUNCRADLE_PLAYWRIGHT_MODULE_DIR=/absolute/path/to/node_modules   # 必须含 playwright@1.62.1 与 package.json
export RUNCRADLE_BROWSER=/absolute/path/to/chrome                       # 必须为 155.0.8059.39
make prepare-web-test web-test
```

- Node.js 仅用于执行开发期测试脚本（`tests/web/web-test.cjs`），RunCradle 产品二进制构建与运行均不依赖 Node.js。
- 浏览器测试结果写入被 Git 忽略的 `.work/w35/web-results.json`，失败或过程截图写入 `TMPDIR`。

---

## 2. 双 Go 工具链完整验证（交付必跑）

`go.mod` 中的最低版本声明不等于实际运行证明。正式交付或完成阶段任务时，必须分别在 **最低支持工具链（`go1.26.0`）** 与 **日常开发工具链** 下跑通全套测试：

### 第一步：使用最低版本工具链 (`go1.26.0`) 验证

```bash
export PATH=/absolute/go1.26.0/bin:$PATH
export GOTOOLCHAIN=local
export GOCACHE="$PWD/.cache/go-build-minimum"

go version   # 确认输出 go1.26.0
make GO=/absolute/go1.26.0/bin/go GOFMT=/absolute/go1.26.0/bin/gofmt verify experiment-test web-test
```

### 第二步：恢复日常开发工具链再次验证

```bash
# 恢复原 PATH 与默认环境后执行
unset GOTOOLCHAIN GOCACHE
make verify experiment-test web-test
```

- **测试真实性原则**：`Makefile` 将构建二进制、临时文件与测试产物写入 `$RUNCRADLE_WORK_DIR`（默认 `.work/`），依赖及编译缓存写入 `$RUNCRADLE_CACHE_DIR`（默认 `.cache/`），并隔离 `GOCACHE`/`GOPATH`/`GOMODCACHE`；同时通过 `make check-tree`（`scripts/check-tree.sh`）拦截任何非白名单文件落入仓库根目录。涉及原生执行语义的验收应使用固定版本的真实 Process Compose 二进制，检查实际测试输出中是否存在跳过或失败；Mock/替身仅用于模拟 HTTP 协议故障或并发竞争边界。

---

## 3. 文档与证据维护规范（Single Source of Truth）

为了防止文档膨胀与状态冲突，全仓严格执行**单一事实维护位置**：

| 信息类别 | 唯一维护文档 | 维护规则 |
| :--- | :--- | :--- |
| **操作用法与 API/CLI 协议** | [使用指南 (USER_GUIDE.md)](USER_GUIDE.md) | 新增或调整 CLI 参数、退出码、HTTP 路由时同步更新 |
| **已开放能力、拒绝集与预算** | [支持矩阵 (SUPPORT.md)](SUPPORT.md) | 区分源码已实现、验证结果与未开放的实验能力 |
| **配置格式、UI 元数据与版本算法** | [工作流格式 (WORKFLOW_FORMAT.md)](WORKFLOW_FORMAT.md) | 维护原生字段、`ui.json` Schema 与 `revision` 规则 |
| **代码包结构、`Engine` 接口与不变量** | [架构与契约 (ARCHITECTURE.md)](ARCHITECTURE.md) | 锚定真实 Go 包路径、结构体与核心数据流 |
| **开发阶段、暂停任务与下一步计划** | [路线与当前进度 (ROADMAP.md)](ROADMAP.md) | 阶段推进、暂停/恢复仅更新此页，绝不到处复制状态 |
| **验收矩阵与阶段证据索引** | [验收与证据索引 (EVIDENCE.md)](EVIDENCE.md) | [历史归档](archive/20261027_legacy_workspace/README.md)中的证据与报告冻结字节，新增验证单独记录并在此索引 |

- **专题与证据维护**：历史专题设计、交接与验证保留在 [归档](archive/20261027_legacy_workspace/README.md)；新验证单独记录源码身份、命令、结果和未覆盖项，再由证据索引链接。

## 4. 离线目录制包与归档验收

先用 `make prepare` 准备固定内核及 Go 模块缓存，再执行：

```bash
make package-unit-test
make package VERSION=0.1.0-dev
RUNCRADLE_BUNDLE_ARCHIVE="$PWD/.work/dist/runcradle-0.1.0-dev-linux-amd64.tar.gz" make package-test
```

制包使用本地工具链，并强制 `GONOPROXY=none`、`GOVCS=*:off` 和 `GOPROXY=off`，覆盖用户私有模块的直连设置，禁用 Go 模块/工具链联网下载，固定 Linux amd64、CGO 关闭和 readonly 模块模式；缺少缓存时失败。默认拒绝 dirty 和覆盖同名产物，本地开发候选可显式使用 `PACKAGE_FLAGS='--allow-dirty --out .work/dist-candidate'`，清单会记录 `source_dirty=true`。`GO=/absolute/go/bin/go` 可选择构建工具链；完整最低版本验证还需将该工具链的 `bin` 放在 PATH 首位。

归档测试从真实 `.tar.gz` 解压，核对全文件 SHA、权限、许可证，并在不含系统 Process Compose 的 PATH 下验证 B-01—B-10。设置 `RUNCRADLE_BUNDLE_TEST=1` 而缺少归档路径会直接失败。实施与证据见 [P1 记录](bundled-engine/IMPLEMENTATION.md)。
