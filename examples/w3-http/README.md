# 浏览器 HTTP 样例 (w3-http)

本样例演示三节点依赖编排与真实退出码捕获：
1. `prepare`：写入初始 payload 文件；
2. `api`：启动后台服务并监听动态 loopback 端口；
3. `check`：发起真实 HTTP 探测并断言接口返回码，写入 `checked` 与 `observed.json`。

三个节点由 Process Compose 统一调度，业务子进程由 RunCradle `_exec` 监控并直接捕获真实退出码。系统需预装 `python3`。

---

## 快速运行流程

在仓库根目录下执行构建并准备固定内核：

```bash
make build prepare
```

### 1. 启动本地工作区宿主

```bash
./.work/bin/runcradle serve \
  --workspace examples/w3-http \
  --process-compose "$PWD/.cache/process-compose/v1.122.0/process-compose"
```

启动后终端前台会输出首行 JSON（包含 `pairing_url`）。在浏览器中打开该配对链接，完成一次性安全认证（换取 `HttpOnly` 会话 Cookie）。

### 2. 导入与首跑（预期失败）

1. 在 WebUI 页面「导入工作流」中，文件路径填入 `process-compose.yaml`，UI 描述填入 `ui.json`；
2. 点击「导入预览」核对依赖图无误后点击「保存」；
3. 选择目标节点 `check`，点击「运行」。在默认参数下，`check` 节点将真实退出并返回退出码 `7`（设计预期的失败演示）。

### 3. 修改配置与复用（成功退出）

- **方式一（修改默认值）**：在节点检查器或元数据源码中，将 `exit_code` 的 `default` 值从 `7` 修改为 `0`，保存新版本后重新运行，即可观察到节点成功退出 `0`；
- **方式二（切换预设）**：保持原配置版本不变，在运行启动台中直接选择 `success` 预设并提交运行；
- **优先级规则**：显式传参（`values`）> 预设值（`preset`）> 默认值（`default`）。切换预设前请注意取消不必要的显式覆盖项。

---

## 存储与安全约束

1. **源文件保护**：保存操作仅写入工作区 `.runcradle/` 私有版本库，源 `process-compose.yaml` 与 `ui.json` 字节绝对不变。
2. **ZIP 导出**：导出包仅包含原生配置文件与可选 `ui.json`，如需脱机执行需另行复制当前目录的 `workflow.py`。
3. **并发注意**：样例生成的临时业务文件保存在样例目录下，请勿在同一工作区并发运行本样例以避免同名文件读写冲突。

