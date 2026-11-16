---
name: runcradle
description: Use when a user asks to create, edit, save, delete, run or inspect RunCradle workflows in DSH, including natural-language requests about task dependencies, data checks, pipelines or reports in a RunCradle workspace.
---

# RunCradle 工作流

把用户的业务需求落实为可在右侧 RunCradle 工作台中管理的工作流。用户只需描述要做什么；工作目录、配置文件、草稿、版本和运行身份由 Agent 根据当前会话及工具返回值管理。

例如：“创建一个检查数据并生成报告的工作流，保存后运行。”

## 从需求到工作流

1. 使用当前会话工作目录，先读该目录的 `AGENTS.md`。业务工具默认省略 `workspace`；用户指定其他目录时再显式传入。连接已运行的服务，工作台和工具共用它的工作流库。
2. 先查看工作流库 `runcradle_workflows({})` 和相关本地配置。新建时按业务命名 YAML 与 UI JSON 文件；配置文件不是前提：若当前目录存在 `process-compose.yaml`、`ui.json` 可参考；文件已删除或目录为空时，直接按需求从零编写。用户指定修改现有工作流时，先用 `runcradle_workflow_get` 获取它。明确可推断的文件名、节点名和输出路径由 Agent 选择。
3. 按需求编写原生 Process Compose 配置及 `version: 1` 的 UI 声明（标题、节点中文标签）。一次性步骤用 `entrypoint` 参数数组；依赖成功后执行用 `process_completed_successfully`。需要生成报告时写入工作区的 `output/`，由准备步骤创建目录。配置中的工作目录和文件引用均留在授权工作区。
4. 先创建工作区内的 `drafts/` 目录，用 `runcradle_workflow_import({file, ui, output_file})` 导入到 `drafts/<业务名>-<唯一后缀>.json`。草稿文件须未被占用；`.runcradle` 等内部目录由服务管理。修复校验问题后再保存；`valid` 与 `run_supported` 分别检查。
5. 用返回的 `draft_file`、`draft_sha256` 调用 `runcradle_workflow_save`，将返回的 **`document_revision` 作为 `draft_revision`**。新建省略工作流 ID；更新传原 `workflow_id` 和读到的当前 `head_revision` 作为 `expected_revision`。保存后保留 `workflow_id`、`saved_revision`，右侧工作台可从工作流库打开同一份配置。
6. 用户要求“运行、执行、测试、验证结果”时，按下一节执行；只要求创建或修改时，完成配置与保存。

## 运行与结果

- 为一次新运行生成并保留唯一 `request_id`；`runcradle_run` 使用保存的 `workflow_id`、以 `saved_revision` 作为 `revision`，并传入目标 `targets`。按需求选择收尾目标，依赖由 RunCradle 执行。
- 受理后用 `runcradle_inspect` 查看 `run_id`。未结束时短暂等待并再次查询，直到 `final=true`；检查节点退出码、`task_result`、`cleanup`、`io_status` 和 `history_integrity`。生成文件时独立核对实际产物。
- 保存或提交结果不确定时先核对原工作流、原版本或原 `request_id`；保留身份，不自动重复保存或换 ID 重跑。审批拒绝时停止并报告原因，按 DSH 当前权限策略执行。
- 汇报工作流名称、保存结果、真实运行状态和输出文件路径。完整 ID 留作后续管理，用户需要时再展示。原生工作台负责图形编辑和运行记录；AI 保存后可重新打开工作台刷新工作流库。

## 删除工作流

用户要求删除库中的工作流时，先 `runcradle_workflows` 按名称找出对应的 `workflow_id`、`head_revision`；名称或范围不明确时先澄清。调用 `runcradle_workflow_delete({workflow_id, expected_revision: head_revision})`，以 `deleted=true`、`mutation=confirmed` 核对完成。版本冲突后刷新列表再处理；响应不确定时核对原 ID，不自动重试。

删除只移除工作流库条目，原 YAML/UI 文件、归档版本和运行记录保留。用户要求清理本地文件与删除库条目时分别处理，不直接读写或删除 `.runcradle` 内部文件。即使源文件或定义已缺失，也可按列表返回的身份删除库条目；缺失定义不影响新建其他工作流。

工具不可见时用 `runcradle_probe` 检查插件，再核对服务与会话授权。编辑、历史版本、精确参数和异常恢复的详细约定见 [工具参考](references/workflow-tools.md)，处理这些情况时按需读取。
