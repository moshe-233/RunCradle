# RunCradle DSH 插件

`dsh-runcradle@0.1.0-alpha.0` 通过 Bundle 加载 `runcradle-host`，提供诊断工具 `runcradle_probe`、四个运行工具、六个工作流工具和可选的 `runcradle` Skill。运行由外部 RunCradle 工作区服务持有，同一授权工作区中的不同会话可以继续管理同一个运行。

兼容目标为本机 Linux、Node >=26.12.1 <27、DSH 0.2.2-rc.1；开发使用 pnpm 12.9.1。Desktop、PTC、远程 subprocess provider 和其他 DSH 版本不在当前兼容声明内。

## 最小 Agent 接入 Demo

在仓库根目录执行：

```sh
node integrations/dsh/scripts/demo.mjs
```

启动器构建 CLI 和插件、准备 Process Compose，并启动 RunCradle 服务及独立的 `runcradle-demo` DSH Profile。默认地址为 `http://127.0.0.1:3085`，浏览器使用终端输出的 DSH 登录链接打开。已有 DSH 安装、Go、Node、pnpm 和 python3 为运行前提。

在 DSH 中选择 **RunCradle Demo** 工作区和已登录的 DeepSeek 模型，再从右侧栏的“开始 / 新标签页”进入 **RunCradle 工作台**。右栏直接嵌入主项目 `internal/webui/` 的原有工作台，沿用它的节点图、编辑器、工作流库、运行历史和证据视图。插件只提供 Agent 工具与工作台入口，没有另一套管理界面或业务 API。

已注册正式命名的 `runcradle` Skill，Agent 可自动发现；工作区指引也会将工作流请求交给该 Skill。可直接发给 Agent：

> 创建一个检查数据并生成报告的工作流，保存后运行。

AI 工具和工作台共用 `.work/demo/workspace` 的工作流库；AI 保存后可重新打开右栏工作台，再从工作流库选择新工作流。也可以让 AI 只写配置，在原工作台中使用“导入工作区文件”完成保存和执行。示例的目标 `report` 会自动执行依赖并写入 `output/report.txt`。

退出启动终端，或在另一个终端执行：

```sh
node integrations/dsh/scripts/demo.mjs --stop
```

关闭仅针对该启动器持有的 DSH 和 RunCradle 进程；工作流、运行记录与 DSH Profile 留在 Git 忽略的 `.work/demo/` 中。再次启动保留这些数据和 Profile 设置。日志与包含一次性配对链接的运行配置均为本地私有文件。RunCradle 首次配对链接沿用主项目的 5 分钟有效期；若尚未打开工作台就过期，请重新启动 demo。

可选环境变量：`RUNCRADLE_DEMO_PORT` 修改 DSH 端口；`RUNCRADLE_CLI`、`RUNCRADLE_PROCESS_COMPOSE` 复用已有二进制（CLI 需支持 `serve --frame-origin`）；首次创建 Profile 时可用 `RUNCRADLE_DEMO_MODEL_PROFILE` 指向现有 `cordis.patch.yml`，只复用其中的 `llm-pi-ai` 与 `agent-default-model` 配置。`--no-open` 只打印地址。常规启动直接在此 demo 中登录 DeepSeek 即可。

`serve --frame-origin http://127.0.0.1:PORT` 只允许指定的本地 DSH Origin 嵌入页面；未配置时仍禁止嵌入。工作台继续使用原有一次性配对、HttpOnly Cookie、同源 API 和 Origin 校验，Agent 工具继续使用已有 CLI 协议和 DSH 授权。

## 安装与配置

```sh
dsh plugin --profile YOUR_PROFILE add /absolute/path/dsh-runcradle-0.1.0-alpha.0.tgz
dsh --profile YOUR_PROFILE --dump-config
```

插件包只包含 JavaScript、类型声明和说明，不内置 RunCradle CLI 或 Process Compose。由用户在另一个终端预先启动服务：

```sh
/absolute/path/runcradle serve --workspace /absolute/path/workspace --process-compose /absolute/path/process-compose
```

在目标 Profile 的 `cordis.patch.yml` 中配置：

```yaml
- id: runcradle-host
  config:
    runcradleExecutable: /absolute/path/runcradle
    enableSkill: true
```

`runcradleExecutable` 可省略，省略时保持探针能力。配置路径后，只有 subprocess 和 workingDirectory 服务可用时才注册业务工具。加载插件本身不解析程序、不启动进程、不访问工作区。`enableSkill` 默认 true；缺少 skills 服务时工具仍可用，恢复后自动注册 Skill。

启用 HMR 时配置变更会重组插件；未启用时重启目标 Profile 后生效。禁用整行使用 `disabled: true`。卸载命令：

```sh
dsh plugin --profile YOUR_PROFILE remove dsh-runcradle
```

## 工具用法

所有业务工具必须在 Agent 上下文中调用。可选 `workspace` 省略时取该 Agent 当前有效工作目录，相对路径也以该目录解析；输入文件相对工作区解析并必须留在该工作区内。切换目录不会扩大原授权范围。

| 工具 | 参数与结果 |
| --- | --- |
| `runcradle_run` | 必填 `request_id`、非空 `targets`；选 `file`（可带 `ui`）或 `workflow_id` + `revision`。可选 `preset`、`values_file`、`timeout_ms`、`retain_evidence`。仅异步提交，受理即返回。 |
| `runcradle_runs` | 可选 `workflow_id`、`offset`、`limit`。在线列表只返回摘要；按返回的 `next_offset` 翻页。 |
| `runcradle_inspect` | `run_id`、`request_id` 二选一。默认摘要；`node`、`nodes_after`、`events_after` 三种视图互斥，支持 `limit`。已知身份时可离线查询。 |
| `runcradle_stop` | 必填 `run_id`。返回停止请求的确认程度；随后 inspect 核对 `final` 和 `cleanup`。 |

例如依次调用：

```json
{"request_id":"build/20261111/1","file":"process-compose.yaml","targets":["build"]}
```

```json
{"request_id":"build/20261111/1"}
```

第一组参数用于 `runcradle_run`，第二组用于 `runcradle_inspect`。新的会话可先调用 `runcradle_runs({})` 找回运行身份。

任务时限默认 300000 ms，范围 1..3600000 ms；分页默认 20，范围 1..100。`request_id` 为 1..128 UTF-8 字节，禁止控制字符，插件不会 trim、替换或自动生成。事件游标必须用十进制字符串（例如 `"9007199254740993"`），避免 uint64 精度损失。完整工具输出限制 64 KiB；列表按完整记录裁剪，详情及事件保留核心的截断、分页与历史缺口标记。运行列表是动态偏移分页，并非跨页快照。

## 工作流管理

六个工具共享可选 workspace；workflow_id、revision、expected_revision、draft_revision和SHA-256均为64位小写十六进制。

| 工具 | 参数与结果 |
| --- | --- |
| `runcradle_workflows` | offset/limit默认0/20，limit1..100；返回workflow_id/head_revision/title/updated_at摘要、next_offset/has_more。 |
| `runcradle_workflow_get` | workflow_id、output_file必填；可选revision读取旧版本；versions_offset/versions_limit默认0/20。分开document_revision与head_revision。 |
| `runcradle_workflow_import` | file、output_file必填，可选ui；生成未入库草稿，源文件不变。 |
| `runcradle_workflow_validate` | draft或draft_file+draft_sha256二选一，output_file必填；可选edit_json或workbench_edit_json二选一。输出完成的草稿和校验摘要。 |
| `runcradle_workflow_save` | draft输入加draft_revision；更新同时传workflow_id/expected_revision，新建两者都不传。先纯校验同一冻结输入，成功返回workflow_id/saved_revision/head_revision/version_count。 |
| `runcradle_workflow_delete` | workflow_id、expected_revision 必填；按当前 head 删除库条目，保留配置源文件、归档版本与运行记录。 |

完整操作与恢复流程见[包内 Skill](assets/SKILL.md)和[工具参考](assets/references/workflow-tools.md#查找编辑保存)：列表找回 → get/import → 编辑/validate → save → 用固定saved_revision运行。get/import/validate必须提前指定新的相对output_file，父目录存在且逐层无符号链接；不覆盖或自动建目录，草稿跨调用/重载保留。Draft仅含format、source、可选ui_source/entry/ui_path；source/UI保存精确文本。文件输入需匹配draft_sha256，修改后重新计算并validate。输入与逻辑路径均限工作区，禁止.runcradle、.git、.agents、.codex、.aws及其别名。

source≤1MiB、UI≤256KiB，Draft JSON/HTTP body≤8MiB。完整文本仅写入草稿文件；聊天总输出≤64KiB，issues最多20个并标记截断，列表按完整项续页。历史最多4096个唯一版本，以首次出现顺序倒序分页；head单独返回，恢复旧内容不删除历史。输出draft_file/draft_bytes/draft_sha256可用于恢复；ABORTED后先核对文件完整性再validate。

edit_json/workbench_edit_json是单个原生Edit/WorkbenchEdit JSON对象的文本，按公开OpenAPI限制字段，不重序列化其中的大整数。原生编辑支持command、entrypoint、working_dir、environment、depends_on；工作台动作含过程增删改名/更新、输入绑定/解绑、预设保存/删除、标题说明修改。非法基础定义或共享YAML通过raw source修复。结构化编辑失败不生成output_file；纯校验或导入可ok=true且valid=false。valid、run_supported、库保存与实际运行成功互不替代。

受限save总需一次DSH审批；workspace-write在授权范围内允许查询和草稿生成；read-only因临时/草稿文件写入需审批。保存mutation为not_attempted、not_saved（确认CAS409）、confirmed或unknown。confirmed与cleanup错误可同时出现，仍保留保存ID。写请求发出后400/404/500、取消、响应丢失及输出失败保守报告unknown，不重试。新建没有request_id幂等保障；相同revision候选不足以恢复唯一创建身份。更新未知时通过get对账，但资源状态不能证明哪次调用完成。409后显式get并处理，不自动改expected_revision重发。

以当前head为expected_revision保存旧Draft即可恢复旧内容；同内容no-op可成功，CAS内容版本不检测A→B→A的所有中间保存。固定revision不冻结外部脚本、环境和文件，也不影响已经提交的运行。

## 权限、恢复与生命周期

每次调用重新解析当前会话权限。缺少 sandboxPolicy 时业务调用失败。受限会话中的 run/stop 必须获得单次 DSH 审批；full access 直接执行。查询也可能写入：在线查询生成临时响应文件，inspect 可能清理过期诊断。因此 read-only 查询需审批，workspace-write 仅在允许写入范围内可直接查询。缺少 approval 服务只阻止需要审批的调用。拒绝或取消审批后不会启动 CLI、创建临时文件或提交运行；批准后还会重新检查工作区与输入文件身份。

审批控制的是对现有服务的操作；工作流使用服务启动者的宿主权限执行，插件不为工作流另建沙箱。CLI 与服务均核验 `v1 / 1.0.0` 及所需能力。在线操作沿用核心实例校验，不额外承诺“原子绑定先前探测的实例提交”。

返回值中的 `ok` 表示本次工具操作是否成功；`result.accepted` 表示提交是否受理，`result.final` 表示是否终态，`task_result`、`reason`、`cleanup`、`io_status`、`history_integrity` 是独立事实。查到业务失败的工作流仍是查询成功。`stop_requested` 只表示停止请求已获确认，不能解释为“已经停止”。`client_cleanup` 仅描述本次 CLI 客户端的回收状态，不能替代运行的 `cleanup`。

相同 `request_id` 与相同意图会返回同一运行；改变意图会冲突。提交响应丢失、工具取消或客户端超时时，先用原工作区及原 `request_id` inspect 对账。插件不会换 ID 或自动重试提交；容量拒绝是 `not_submitted`，不表示排队。DSH 取消管线可能把工具结果显示为 `ABORTED`，因此调用前就应保存 workspace/request_id。

每个 CLI 客户端预算 60 秒，终止宽限 3 秒，再观察至多 5 秒；无法确认回收时报告不确定。API 响应文件最多读取 32 MiB，并核对路径、长度及 SHA-256。并发调用使用独立句柄和临时目录；仅清理本次拥有的文件，回收未确认时保留路径供排查。

工具取消、Agent 销毁、插件卸载和 DSH 退出只回收插件的 CLI 客户端，不追加 stop。外部服务及运行仍由原启动者管理。服务不可用时列表和 stop 报错，inspect 可读取已有记录；插件不会自动启动服务，也不会扫描私有存储来生成离线列表。

## 开发与验证

在本包目录执行：

```sh
pnpm install --frozen-lockfile
pnpm run typecheck
pnpm run test
pnpm run test:package
pnpm run test:smoke
env RUNCRADLE_CLI=/absolute/path/runcradle RUNCRADLE_PROCESS_COMPOSE=/absolute/path/process-compose pnpm run test:runtime
```

CLI 在仓库根目录用 `make build` 构建；`make prepare` 下载并校验固定引擎，`make check-tree` 检查产品目录边界。`test:runtime` 缺少真实 CLI 或引擎直接失败，不算跳过后通过。

`test:package` 检查发行内容和解包调用；`test:smoke` 验证探针、真实 Agent 作用域、HMR、安装与卸载；`test:runtime` 使用真实 DSH Agent、subprocess provider、CLI 和引擎验证工作流与运行工具。后两者创建隔离的 DSH_HOME/Profile，不改日常配置。测试驱动拥有并清理测试服务，产品插件始终只连接已有服务。

`RUNCRADLE_DSH_TEST_DIR` 指定证据父目录，默认系统临时目录。保留日志、verification.json、tarball 和 SHA256SUMS。测试不调用外部模型，自动化通过与人工使用验收分别记录。复制/导出、服务自动管理、有限等待、主动通知、离线列表和随包程序交付留待后续。

删除可直接对 Agent 说“删除名为 XXX 的工作流”。Skill 会先查询库并核对范围，再通过 `runcradle_workflow_delete` 操作；名称重复或范围不清时会澄清。删除有版本比较，409 后需刷新，未知结果不自动重试。工作流的定义文件缺失时，库列表仍返回元数据，避免卡住其他工作流的创建与管理；新建也不依赖示例文件是否存在。
