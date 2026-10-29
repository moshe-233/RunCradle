# RunCradle Linux amd64 目录发行包

RunCradle 提供开箱即用的 Linux `amd64` 自包含目录发行包。解压整个目录后，使用普通用户从任意路径直接启动宿主：

```bash
/path/to/runcradle-<version>-linux-amd64/runcradle serve --workspace /path/to/project
```

- **内置固定内核**：包内自带 `libexec/process-compose`（固定官方版本 `v1.122.0`），无需在系统安装 Process Compose、Go 工具链或 Node.js；
- **业务环境自理**：用户工作流中声明的 Python、Docker、Shell 脚本等业务依赖仍由宿主环境或工作区提供；
- **离线与权限要求**：运行时绝不联网下载任何文件；发行包所在目录可设为只读，目标工作区（`--workspace`）需具备普通用户读写权限（用于管理 `.runcradle/` 私有库）。

---

## 1. 完整性校验与许可证清单

在解压与使用前，建议先执行校验：

```bash
sha256sum -c runcradle-<version>-linux-amd64.tar.gz.sha256
```

- **清单清单 (`bundle.json`)**：记录构建源码提交哈希、工作树污染状态（`source_dirty`）、内核 SHA-256 指纹以及除清单本身外全部分发文件的 SHA-256 校验和；
- **合规声明 (`licenses/`)**：包含 RunCradle 依赖库及上游 Process Compose 依赖的许可证全文与来源清册。

---

## 2. 零业务依赖自检流程

将测试样例复制到独立工作区进行自检：

```bash
# 复制样例文件到工作区
cp /path/to/runcradle-<version>-linux-amd64/examples/hello.yaml /path/to/project/

# 终端一：启动宿主
/path/to/runcradle-<version>-linux-amd64/runcradle serve --workspace /path/to/project

# 终端二：在另一个终端执行成功与预期失败运行
/path/to/runcradle-<version>-linux-amd64/runcradle workflow run \
  --workspace /path/to/project --file hello.yaml --target success --request-id hello-success

/path/to/runcradle-<version>-linux-amd64/runcradle workflow run \
  --workspace /path/to/project --file hello.yaml --target failure --request-id hello-failure

/path/to/runcradle-<version>-linux-amd64/runcradle workflow inspect \
  --workspace /path/to/project --request-id hello-failure
```

- **预期结果**：`hello-success` 任务真实退出 `0`；`hello-failure` 任务真实退出 `7`（用于验证 `_exec` 直接子进程失败回执）。
- **浏览器访问**：首次连接 WebUI 请使用宿主在终端首行输出的 `pairing_url` 一次性凭证链接。

---

## 3. 内核解析优先级

系统按以下确定性顺序解析执行引擎：
1. **显式指定路径**：CLI 传入 `--process-compose PATH`（路径为空或文件不存在时立即拒绝启动，绝不隐式回退）；
2. **随包携带内核**：从当前可执行文件真实所在目录的 `libexec/process-compose` 定位；
3. **系统环境兜底**：仅限**纯源码构建且无随包标记**的二进制，才允许从系统 `PATH` 寻找内核。

> [!CAUTION]
> 若目录包缺少 `libexec/process-compose`、清单哈希不匹配或内核 SHA 错误，宿主会在打开网络与初始化存储前拒绝启动并退出（退出码 `2`）。单独拷贝 `runcradle` 主程序无法构成有效安装。命令软链接可安全指向完整安装目录内的 `runcradle`。

---

## 4. 平滑升级与安全回退

1. **版本并列**：解压新版本至平行的独立目录（例如 `runcradle-0.2.0-linux-amd64/`）；
2. **有序收尾**：向旧宿主进程发送 `SIGINT` 或 `SIGTERM` 信号，宿主将在 **12 秒**总预算内有序停止所有持有的运行实例并退出；
3. **切换软链接**：将系统的命令软链接切换至新宿主程序，并启动新宿主；
4. **版本回退**：保留旧版本目录；如需回退，先确认旧程序与当前工作区数据格式兼容，再反向切换软链接即可。升级或回退绝不会覆盖运行中的目录，也不会自动删除历史运行数据。

---

## 5. 权限与安全边界

- 随包内核通过本目录私有路径调用，但每次任务运行依然在独立的私有临时目录与 UDS 空间启动独立实例；
- 所有进程均在当前普通用户权限下运行，目录包**不提供容器沙箱或提权隔离**。

