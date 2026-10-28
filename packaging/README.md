# RunCradle Linux amd64 目录包

解压整个目录，使用普通用户从任意目录启动：

```bash
/path/to/runcradle-<version>-linux-amd64/runcradle serve --workspace /path/to/project
```

包内 `libexec/process-compose` 是固定 v1.122.0 内核。无需安装系统 Process Compose、Go 或 Node；运行工作流自己的 Python、Docker 等业务依赖仍由项目提供。启动不会联网下载文件。包目录可只读，工作区需要可写。

先校验归档：`sha256sum -c runcradle-<version>-linux-amd64.tar.gz.sha256`。
`bundle.json` 记录源码提交、是否 dirty、内核指纹及除清单自身外所有文件的 SHA-256。`licenses/` 包含 RunCradle 依赖和内核依赖的许可与来源清单。

零业务依赖自检：把 `examples/hello.yaml` 复制到独立工作区，启动上述宿主，在另一个终端运行：

```bash
/path/to/runcradle workflow run --workspace /path/to/project --file hello.yaml --target success --request-id hello-success
/path/to/runcradle workflow run --workspace /path/to/project --file hello.yaml --target failure --request-id hello-failure
/path/to/runcradle workflow inspect --workspace /path/to/project --request-id hello-failure
```

两个任务分别真实退出 0 和 7；后者用于验证失败回执。首次浏览器连接使用宿主终端输出的配对链接。

引擎选择优先级：显式 `--process-compose PATH`（空值或错误立即拒绝）→ 本安装目录内核 → 仅纯源码构建且无随包标记时使用 PATH。安装缺失、清单不匹配或内核哈希错误时重新解压完整包；单独复制主程序不能组成安装。`validate`、`render`、`inspect`、`version` 和 `help` 不需要内核；`validate --check-engine` 会核对它。命令软链接可指向完整安装目录里的 `runcradle`。

升级时解压到另一个版本目录，向旧宿主发送 SIGINT/SIGTERM 并等待退出（收尾预算 12 秒），再切换命令软链接并启动新宿主。保留旧目录用于程序回退；回退前确认旧程序兼容当前工作区数据。不覆盖正在运行的目录，也不自动迁移或删除历史。

内核由本目录私有路径选定，各运行仍有自己的实例和运行目录。所有命令使用当前用户权限，包不提供容器或权限沙箱。
