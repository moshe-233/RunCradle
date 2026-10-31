# RunCradle v0.1.0

RunCradle 的首个正式发行版本：在本地浏览器中编辑、保存和运行 Process Compose 工作流，通过 CLI 和 HTTP/SSE 接口复用同一套工作区与运行记录。

发布日期：2026-10-31。支持 Linux amd64、非 root 用户运行。

## 本版提供

- **本地工作台**：单文件 JSON/YAML 导入、依赖图、节点与源码编辑、参数和预设、独立三主题、运行状态与事件查看。
- **版本保存与复用**：配置保存在 `.runcradle/` 私有库，保存检查 `expected_revision`，运行绑定不可变版本，源文件保持原样，配置可导出 ZIP。
- **显式运行管理**：每个工作区一个前台宿主，固定 request ID 幂等提交，支持受理后返回、查询、显式停止和离线历史检查。
- **可检查的运行事实**：记录直接子进程退出码与信号，区分引擎结果、历史完整性与清理范围；实时事件支持游标恢复。
- **公共接入接口**：CLI JSON、`/api/v1` HTTP/JSON 与 SSE；契约版本 `1.0.0`，内嵌 OpenAPI `3.1.1`，提供 `api describe/schema/status/request`。
- **离线目录包**：包含 Process Compose `v1.122.0`、SHA-256 清单、依赖许可证和成功/失败自检样例。无需安装 Go、Node.js 或系统级 Process Compose。

## 下载与校验

下载 Release 附件中的以下两个文件：

- `runcradle-0.1.0-linux-amd64.tar.gz`
- `runcradle-0.1.0-linux-amd64.tar.gz.sha256`

```bash
sha256sum -c runcradle-0.1.0-linux-amd64.tar.gz.sha256
tar -xzf runcradle-0.1.0-linux-amd64.tar.gz
cd runcradle-0.1.0-linux-amd64
./runcradle version
./runcradle serve --workspace /path/to/project
```

工作区须提前创建并允许当前用户读写。使用启动 JSON 中的 `pairing_url` 打开工作台。完整安装、自检、CLI/API 使用与升级回退说明位于归档内 `README.md`；请保留整个目录，不要只拷贝主程序。

## 支持边界与已知观察

- 当前执行范围是单文件 Process Compose 配置的受限子集；多文件合并、`extends`、持续会话、自动触发、热更新和远程监听尚未开放。Agent 插件、MCP 服务与公共语言 SDK 不包含在本版中。
- 工作流自身的业务依赖需要另行准备。所有任务使用当前用户权限运行，不提供容器沙箱；清理结果只覆盖登记的进程组与私有临时目录。
- `workflow run --detach` 退出 `0` 仅表示受理；`inspect` 退出 `0` 仅表示查询成功。节点失败时，CLI 返回 `1`，真实节点退出码在结果中。
- 历史人工试用曾观察到事件 `gap`，以及服务节点收到 SIGTERM 后 `failed` / `stopped` 标签不同。本版继续如实暴露历史完整性、信号与清理事实，不将这些观察项标为已解决。
- 自动化测试结果与人工体验验收分别记录；本次发行不新增人工 UI 验收结论。

## 验证范围

发行验证覆盖完整 Go 检查、真实引擎集成、race、7 套浏览器测试、制包单元测试，以及正式归档解压后的运行、只读安装、引擎损坏拒绝、离线查询和版本切换。具体构建身份由归档内 `bundle.json` 的 `source_commit` 与 `source_dirty` 记录。
