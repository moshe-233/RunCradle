# 随包内核验证记录

2026-10-28。结论：源码与候选包已实现，B-01—B-10 归档验收通过；终审修正后的双工具链完整回归全部通过。未执行远端 CI、推送、合并或 release 发布。

## 源码与产物身份

基线 `d9c13f993c0f523cc1470d1dabbf4e006893037c`，分支 `codex/bundled-engine`。本次为未提交工作差异，候选包明确记录 `source_dirty=true`，不能把基线提交当作候选包的完整源码身份。129 个产品/许可文件的差异 SHA 清单在 `.work/bundled-engine/source-manifest.json`。

| 构建工具链 | 候选归档 | SHA-256 | 文件数 |
| --- | --- | --- | --- |
| Go 1.26.0 | [本地候选包](../../.work/bundled-engine/final-minimum-dist/runcradle-0.1.0-dev-linux-amd64.tar.gz)，23789749 bytes | `c609273b6becaf6a5446e623652e1c3c77695b4247fd8a27cf9b00eb2d988771` | 116 |
| Go 1.27.3-X:nodwarf5 | [本地候选包](../../.work/bundled-engine/final-dev-dist/runcradle-0.1.0-dev-linux-amd64.tar.gz)，25061205 bytes | `9af5c40f541c85d6b07cddd75141234686d92229830748f1ecfaa6c660337493` | 116 |

两个归档均由各自工具链重新构建；通过 `go version -m` 实读二进制确认，结果见 `.work/bundled-engine/final-{minimum,dev}-buildinfo.txt`。包 SHA sidecar 与全文件 manifest 均核对通过。

## 验证命令与结果

| 验证层 | 实际结果与日志 |
| --- | --- |
| 修改前基线 | `make test` 通过，`baseline.log` |
| 固定身份、解析、CLI | 均有 RED→GREEN；定向包测试通过，原始日志保留 |
| 制包单测 | 10/10，`final-packaging.log`；覆盖版本/dirty/ELF/清单/覆盖/许可完整性/离线私有代理/校验文件写入失败 |
| 最低 Go 完整验证 | `make verify web-test package-unit-test` 全部通过；18 个 graph 断言、前端专项和浏览器主线 14/14，`final-minimum-reviewed.log` |
| 开发 Go 完整验证 | `make verify web-test package-unit-test` 全部通过；18 个 graph 断言、前端专项和浏览器主线 14/14，`final-dev-reviewed.log` |
| 真实归档测试 | 两套工具链候选包的 `make package-test` 均通过，各 4 组及 9 个损坏子场景，`final-{minimum,dev}-bundle.log` |
| 宿主测试时序 | 原 race 失败复现，测试独立等待 active handle 释放后 `-race -count=100` 通过，`host-release-green.log` |
| 文档/树检查 | `git diff --check`、`make check-tree` 与当前文档本地链接检查通过 |
| 独立审查 | Sol/max 一次 whole-change 审查，无 Critical；4 项 Important 已在一次修正中处理，无遗留 Minor；不做重复审查 |

所有日志位于 `.work/bundled-engine/`。全套 `make verify` 包含 fmt/tree/vet、普通和 race 全部 Go 测试、真实固定内核集成、模块校验与构建；浏览器额外执行 graph、frontend 和原有主线。集成 TestMain 使用 PATH 中的 go，因此最低版本验证同时设置 PATH、GO 和 GOFMT。

## B-01—B-10

| 编号 | 真实归档断言与结果 |
| --- | --- |
| B-01 | 无系统内核 PATH 下直接启动随包宿主：通过 |
| B-02 | 中文/空格目录、不同 cwd、外部命令软链接：通过 |
| B-03 | 正常/损坏包均不执行 PATH 同名标记替身：通过 |
| B-04 | 缺引擎/清单、无效或超限清单、身份错误、无执行权限、错误架构、篡改与越界链接均启动前失败；新工作区无私有目录：通过 |
| B-05 | 显式绝对/相对路径覆盖、空值/错误拒绝：通过；源码 PATH 另有解析器行为测试 |
| B-06 | 真实节点退出 0/7，direct_receipt、task_result、固定引擎 SHA 和 registered_process_groups 清理事实独立核对：通过；不要求或伪造完整事件历史 |
| B-07 | 损坏引擎时 validate/render/inspect/version/help 正常，check-engine 拒绝：通过 |
| B-08 | 0555 安装目录可运行；只复制主程序拒绝：通过 |
| B-09 | 停止旧宿主，另一完整目录接手同工作区并读取旧历史、执行新任务：通过；这是同候选版本的目录切换，不是跨数据格式降级兼容性证明 |
| B-10 | 实际 tar 单顶层目录、无链接、权限、全文件 SHA、许可证、声明链接及校验文件：通过 |

## 终审修正与既有观察

1. 离线制包不能仅依赖 GOPROXY=off：私有模块例外会绕代理进入 direct VCS。真实 Go 缺缓存测试先观察到 direct 分支，再强制 GONOPROXY=none 与 GOVCS=*:off 后观察到 GOPROXY=off 拒绝；测试中通过 GOVCS guard 阻止真实网络。
2. 源码的 third_party/licenses 链接不适用于包内 licenses 布局：真实旧归档链接检查 RED，制包重写声明链接后新归档 GREEN。
3. 校验文件写入失败会留不完整 sidecar：真实临时输出加磁盘写入故障注入 RED→GREEN；失败清理本次归档和 sidecar，预先存在的 sidecar 保留。
4. Probe 临时目录 I/O 错误原被误归为安装错误 2：resolver 与 serve/check-engine 回归 RED→GREEN，现标记 ErrInternal 并返回 125，仍保持工作区零副作用。

原始失败反例保留。最初样例发现固定上游加载期会查找默认 Bash，即使节点声明 entrypoint；样例现显式声明项目 shell 与进程入口 /bin/sh，不改动内核。许可证文件名收集中过宽的匹配曾误带源码，已移除，固定二进制实际 85 个模块对应 102 份依赖许可/NOTICE；主许可证与 Go runtime 许可另行附带。

宿主原有 race 测试读取最终历史后立即断言 active 表为空，与实际异步释放顺序不符。本轮仅修正该测试为最多 2 秒独立等待；持续泄漏仍失败，未修改宿主生产实现。原失败见 `final-dev.log`。

## 来源与范围裁决

设计固定的上游 tag/reference commit 为 `673850dd20683ef14b33890444ca3416d02c751c`；SHA 匹配的官方发布二进制嵌入 `vcs.revision=23b0acacc937d745279fb1551337f4031c4fc865`。两者分别记录，不将其当作同一源码身份。已直接比较两个提交的 LICENSE，字节相同，SHA-256 为 `3a8b61ac54f1969a21279c9026641d28b7523ed22cea75fadc44fddadc1a8ceb`。运行选择继续强制批准的二进制 SHA，完整来源见 `third_party/licenses/process-compose/inventory.json`。审查者独立按实际 buildinfo 核对了 85 个依赖，并逐字比对 102 份许可与对应模块 ZIP。

沿用当前专题分支，历史设计和证据不改写，scratch 在 .work。用户选定的显式替代引擎是受信任程序；同用户主动替换安装、不可中断远端文件系统 I/O 和任意程序沙箱不是本次边界。运行时核对产品身份和引擎，完整材料清单在制包/B-10 校验；源码/显式引擎按既有约定确认版本并记录实际 SHA。允许本地安装中的包内链接，制包不生成链接；manifest 额外元数据不改变执行授权字段。

正常失败清理有回归，未扩展为断电/SIGKILL 事务发布；不承诺所有用户 Go 编译选项下字节可复现。失效 cwd 返回内部错误，发行目标限定 Linux amd64 静态二进制。业务工具由项目提供。许可覆盖与来源验证是工程证据，不是独立法律或完整供应链审计。

W4a 保持暂停；W3.5 人工/视觉验收、真实用户可用性、远端 CI 与发布仍为独立事实。没有改动生产调度、nodeexec、workflow schema、运行存储。docs 沿用既有忽略策略，本地文档不等于已随 Git 提交交付。

后续状态更新（2026-10-28）：用户已明确确认 W3.5 人工验收完成，记录于[工作台实施记录](../design/webui-style/IMPLEMENTATION.md)。上文待验收描述为本次 P1 自动化验证时的状态；P1 候选包试用及远端验证不据此标记完成。

Git 收尾更新（2026-10-28）：用户要求将本阶段改动合并至 main。依据 AGENTS.md 的同提交文档契约，本阶段相关现行文档将显式纳入提交；保留整目录 ignore 规则，历史归档、概念图、缓存及验证产物不批量纳入。上文关于文档仅在本地的描述为合并前状态。
