# RunCradle 工作流与运行管理

需要在 DSH 中查找、导入、编辑、保存工作流、执行版本、找回运行或查询结果时使用本说明。用户和 Agent 共享工作区服务的工作流库与运行记录。

先确认十个业务工具可见；它们必须在 Agent 上下文中调用，并按当前会话工作目录、权限策略和所需审批执行。`runcradle_probe({})` 可检查插件加载，`echo` 可选；返回版本与原样 echo。探针不检查服务健康，也不启动进程或访问工作区。业务工具需要配置绝对路径 `runcradleExecutable`，以及宿主的 subprocess、workingDirectory、sandboxPolicy；服务由用户预先启动。缺少工具时请核对 Profile 配置与 Agent 工具限制。

## 查找、编辑、保存

1. 新会话先 `runcradle_workflows({})`，用 `next_offset` 翻页。库由已有服务管理，服务不可用时不能离线枚举。
2. `runcradle_workflow_get({workflow_id, output_file})` 取 head；加 `revision` 可读历史。`document_revision` 是读到的版本，`head_revision` 是当前 head；更新时 expected_revision 来自当前 head。历史以首次出现顺序倒序分页，versions_offset/versions_limit 默认0/20，limit 1..100；不是保存操作审计日志。
3. 从文件开始用 `runcradle_workflow_import({file, ui?, output_file})`。导入只生成草稿，不入库、不写回源文件。
4. `runcradle_workflow_validate` 接受 `draft` 对象或 `draft_file` + `draft_sha256` 二选一，以及新 `output_file`。Draft 只含 format、source、可选 ui_source/entry/ui_path。原生文本保持精确字符串；可直接编辑草稿中的 source，编辑后计算新 SHA-256 并重新 validate。
5. 结构化修改可额外传 `edit_json` 或 `workbench_edit_json`，不能同时传；它们是现有 OpenAPI Edit/WorkbenchEdit 对象的 JSON **文本**。整数 default/preset values 如9223372036854775807需保持原始数字文本，不能先转 JS Number。native edit支持process/field/value/remove；workbench支持过程增删改名/更新、参数绑定、预设和元信息；共享 YAML 或非法基础定义需 raw source 修复。例：`workbench_edit_json: '{"action":"update_meta","title":"Build"}'`。
6. `runcradle_workflow_save` 接受完成的 Draft 输入和 `draft_revision`（来自get/import/validate）。新建省略 workflow_id/expected_revision；更新必须同时传这两个字段。save再次纯校验并提交同一冻结快照，不接受编辑参数。成功返回 workflow_id、saved_revision、head_revision、version_count。
7. 执行时将保存返回的 workflow_id + saved_revision 传给 runcradle_run 的 workflow_id + revision。固定版本只冻结定义与UI声明，不冻结引用的外部脚本、程序或环境。

get/import/validate 必须提前指定一个未占用的相对 output_file（≤512 UTF-8字节，父目录已存在且无symlink）。草稿以0600权限保留完整文本；返回 draft_file/draft_bytes/draft_sha256，正常收尾与插件重载不删除它。source≤1MiB、UI≤256KiB、Draft JSON/请求正文≤8MiB；聊天不回显完整正文，总结果≤64KiB，诊断最多20项并带截断标记。文件和逻辑entry/ui_path必须留在工作区，不能进入.runcradle、.git或其他受保护目录，包括别名。

`ok=true` 仅表示工具操作成功，import/纯validate可同时 `valid=false`，便于修复；结构化编辑被拒绝则不发布新文件。valid、run_supported、保存成功、实际运行成功分别判断。文件存在不单独证明完成，ABORTED后检查完整JSON、字节和哈希并重新validate；保留调用前的output路径和版本身份以便恢复未保存草稿。

save的 `result.mutation`：not_attempted表示未派发写入；已验证CAS409为not_saved，显式get当前head后处理冲突；confirmed保留保存身份，清理失败仍可能ok=false；unknown表示可能已写入，不自动重试。新建无request_id幂等保证，响应丢失后同revision候选不能证明唯一ID。更新未知时当前head或历史含目标revision只能证明内容状态，不能证明本次调用成功。400/404/500、超时或输出失败都不能推断未保存。恢复旧版本需get旧Draft，再用**当前head**做expected_revision保存；保留历史，不改变正在运行的版本。CAS允许相同内容no-op及A→B→A，不提供每次保存序号。

full access直接执行；受限save需单次DSH审批，查询/草稿生成在workspace-write授权写入根内直接执行，read-only需审批。输入正文在授权后读取一次并检查SHA-256，调用期间不要修改输入或目录。工具不自动重试、合并、运行、启动服务或写回原文件。

## 提交与找回

- `runcradle_run`：调用前保存稳定 `request_id`（1..128 UTF-8 字节、无控制字符）与工作区。必填非空 `targets`，选择 `file` 或 `workflow_id` + `revision`；只有 file 来源可传 ui。可选 preset、values_file、timeout_ms、retain_evidence。工作流文件、UI 文件、参数文件均需位于工作区内。
- 始终异步提交。受理不表示任务完成。超时默认 300000 ms，可设 1..3600000；这是任务时限。
- `runcradle_runs`：同一授权工作区的新会话可用它找回 run_id。可按 workflow_id 过滤，以返回 next_offset 翻页；limit 默认 20，范围 1..100。列表仅摘要，服务不可用时不能离线枚举。
- `runcradle_inspect`：按 run_id 或 request_id 二选一查询，服务退出后仍可查已知记录。默认摘要；node、nodes_after、events_after 三种视图互斥。事件游标始终传十进制字符串，不转成浮点数。尊重截断、分页、历史缺口和诊断过期标记。

省略 workspace 时使用 Agent 当前有效工作目录；相对 workspace 也以该目录解析。调用期间不要改变工作目录或输入文件；身份变化会要求重新调用。

## 结果与停止

`ok` 是操作是否成功，`result.accepted` 是运行受理事实，`result.final` 是终态事实；业务结果 task_result、原因 reason、运行清理 cleanup、输出 io_status、历史 history_integrity 分别解读。查询到失败任务仍可 ok=true。client_cleanup 仅描述短命 CLI 客户端回收，不代表工作流清理。

`runcradle_stop({run_id})` 请求停止，不自动等终态。收到 stop_requested 后使用 inspect 核对 final 与 cleanup，不能提前称“已停止”。容量拒绝 not_submitted 不表示排队。

相同 request_id 的相同意图复用同一运行，修改意图会冲突。响应丢失、超时、ABORTED 或提交是否成功不确定时，先 inspect 原工作区与原 request_id。不要自动换 ID 重跑。取消工具、销毁会话、卸载插件或退出 DSH 只回收 CLI 客户端，不会自动 stop 已受理运行；外部服务由原启动者管理。

## 权限与边界

受限会话的 run/stop 需 DSH 单次审批；full access 直接执行。列表会创建临时响应文件，inspect 可能清理过期诊断，因此 read-only 查询也需要审批；workspace-write 查询只在授权写入范围内直接执行。目录切换不会扩大原授权，拒绝后不得继续执行。审批控制服务操作，工作流仍使用服务启动者的权限。

业务工具依赖 Agent 上下文，不能用探针成功替代权限或服务验证。CLI 和宿主需兼容 v1 / 1.0.0；插件沿用核心实例检查，不保证原子绑定先前探测实例。尚未提供复制/导出、自动服务管理、有限等待、主动通知、SSE 和随包程序交付。不要声称这些能力已实现。

## 删除库条目

`runcradle_workflow_delete` 必填 `workflow_id` 与 `expected_revision`（新鲜列表的 `head_revision`）。CLI 和宿主必须声明 `workflow_delete` 能力，受限会话每次删除需 DSH 审批。通过公开 DELETE API 移除一个库条目，保留源文件、归档定义、运行记录及正在执行的实例；即使定义文件缺失也可按库元数据删除。

结果包含 `workflow_id`、`deleted_revision`、`deleted=true`、`mutation=confirmed`。未派发为 `not_attempted`；服务明确返回 CAS409 或不存在404为 `not_deleted`；派发后响应丢失或未验证的失败为 `unknown`，不能重试。清理失败与已确认删除可同时出现，需保留删除身份。新建工作流不依赖既有示例，修改已保存工作流使用 get 的归档 Draft，而非依赖原始 YAML/UI 文件。
