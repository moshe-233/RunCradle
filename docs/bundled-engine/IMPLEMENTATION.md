# 随包内核实施记录

2026-10-28。承接已定稿的[设计](../archive/20261027_legacy_workspace/topics/bundled-engine/DESIGN.md)和[历史实施计划](../archive/20261027_legacy_workspace/topics/bundled-engine/PLAN.md)，按当前标准 Go 根目录布局落地。原始归档不改写。

基线：`d9c13f993c0f523cc1470d1dabbf4e006893037c`，分支 `codex/bundled-engine`，初始 tracked tree 干净。源码改动只涉及构建身份、内核解析、CLI 入口、制包与测试；不修改调度、nodeexec、工作流 schema 或运行存储。

- [x] Task 1：统一 `pinned.json`；准备脚本读取同一身份；清点固定二进制内的 85 个 Go 依赖模块并收集许可证和 NOTICE。
- [x] Task 2：实现显式路径 → 随包 → 源码 PATH 的解析顺序；拒绝空值、不完整安装、越界链接和哈希错误；执行前验哈希；有界取消与探测。
- [x] Task 3：注入构建身份；serve 在打开宿主前解析；显式引擎校验共享解析器；离线命令不依赖安装。
- [x] Task 4：Python 标准库离线构建与制包；SemVer、dirty、覆盖、ELF、许可证、权限及全文件 SHA 清单检查；生成候选包。
- [x] Task 5：真实归档 B-01—B-10、最低 Go 1.26.0 与开发 Go 1.27.3-X:nodwarf5 的完整回归均通过；一次独立终审的 4 项 Important 已 RED→GREEN 修正；CI 配置和当前文档同步。远端 CI 尚未执行。

实现裁决：沿用当前已存在且干净的专题分支，不另建缺少忽略文档的 checkout；scratch 统一在 `.work/bundled-engine/`。历史计划旧目录按当前根目录布局解释。零依赖样例显式声明 `/bin/sh`，因为固定上游即便使用进程 entrypoint 也会在加载期查找默认 Bash。

测试、归档身份及未覆盖边界见[验证记录](VALIDATION.md)。`docs/` 沿用既有忽略策略，当前文档修改不会自动进入 Git diff。

Git 收尾更新（2026-10-28）：用户要求将本阶段改动合并至 main。依据 AGENTS.md 的同提交文档契约，本阶段相关现行文档将显式纳入提交；保留整目录 ignore 规则，历史归档、概念图、缓存及验证产物不批量纳入。上文关于文档仅在本地的描述为合并前状态。
