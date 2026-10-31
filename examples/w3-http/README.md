# 三节点 HTTP 工作流样例

这个样例演示依赖启动、HTTP 就绪检查、运行参数与真实退出码。需要 `python3`；下列构建命令在仓库根目录执行。

| 节点 | 行为 | 前置条件 |
| --- | --- | --- |
| `prepare` | 写入初始 payload 文件 | 无 |
| `api` | 在动态 loopback 端口提供 HTTP 服务 | `prepare` 成功完成 |
| `check` | 发起 HTTP 请求，写入 `checked`、`observed.json` 并按参数退出 | `api` 就绪检查通过 |

Process Compose 负责依赖调度，RunCradle 记录直接业务子进程的退出码与信号。默认 `check` 退出 `7`，用于演示失败路径。

## 启动与浏览器操作

```bash
make prepare build
./.work/bin/runcradle serve \
  --workspace examples/w3-http \
  --process-compose "$PWD/.cache/process-compose/v1.122.0/process-compose"
```

打开启动 JSON 中的 `pairing_url` 完成一次性配对，保持宿主终端运行。

1. 在工作台导入 `process-compose.yaml`，UI 描述填 `ui.json`，预览后保存版本。
2. 选择目标 `check`，使用默认参数运行；预期节点真实退出 `7`。
3. 选择 `success` 预设再次运行；预期节点真实退出 `0`。也可以把参数 `exit_code` 的默认值改为 `0`，应用到草稿、保存新版本后运行。

显式参数值优先于预设，预设优先于默认值。切换预设时，先移除不需要的显式覆盖值。收到服务节点的终止回执时，请把它与目标 `check` 的执行结果分开判断。

## 使用 CLI 验证

保持宿主运行，在另一个终端进入仓库根目录：

```bash
# 失败路径：check 的真实退出码为 7，CLI 退出码为 1
./.work/bin/runcradle workflow run \
  --workspace examples/w3-http --file process-compose.yaml --ui ui.json \
  --target check --request-id http-failure

# 成功路径：check 和 CLI 均退出 0
./.work/bin/runcradle workflow run \
  --workspace examples/w3-http --file process-compose.yaml --ui ui.json \
  --target check --preset success --request-id http-success

./.work/bin/runcradle workflow inspect \
  --workspace examples/w3-http --request-id http-success --node check
```

再次执行同一条命令会查询或返回同一次运行；想重新执行，应换 request ID。`failure8` 预设可验证节点退出 `8`，其 CLI 退出码仍为 `1`。

## 文件与并发

工作台保存只写入 `.runcradle/`，不修改导入的 `process-compose.yaml` 与 `ui.json`。配置 ZIP 不包含业务脚本；迁移样例时需一并复制 `workflow.py`。

样例会在工作区写入业务文件，请勿在同一工作区并发运行，以免同名文件互相覆盖。使用发行包时，可把本目录的配置与脚本复制到独立工作区，再用发行包中的 `runcradle serve --workspace DIR` 启动，无需额外指定引擎路径。
