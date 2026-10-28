# 可读性重构验证记录

2026-10-28；源码分支 `codex/readability-refactor`，批准基线 `fea20fd`。本记录区分源码重构、自动化、截图比较及用户人工验收。

## 已完成的阶段

| 阶段 | 源码提交 | 验证 |
| --- | --- | --- |
| 实施前基线 | `fea20fd` | `make verify web-test` 通过；普通/race 真实内核、graph 18、前端专项、浏览器主线 14/14 |
| CLI | `61ff182` | 来源互斥和宿主错误优先级在改动前后通过；unit-test、真实 integration-test 通过（76.966s） |
| 运行服务 | `a0842ab` | unit-test、真实 integration-test 通过（76.827s）；service/httpapi/cli `-race -count=5` 通过；受理关闭竞态、worker 前取消和摘要编码覆盖 |
| 在线前端 | `7cde28e` | unit-test、web-test 通过；模块路由/MIME 的 RED 404 → GREEN；单次点击仅产生一次编辑请求 |

## 界面比较

同一浏览器 Chrome for Testing 155.0.8059.39、Playwright 1.62.1。Browser plugin not available，使用仓库固定版本测试。桌面 1585×992、平板 1024×768、手机 390×844，以及深色模式、启动台、离线页共六张截图，重构前后像素完全一致。没有调整 CSS、DOM 标识或既有视觉设计。

检查器关闭重开/跨节点缓存/丢弃、应用与改名、响应式焦点、主题持久化、参数原文精度、冲突、运行真实退出 0/7、历史和离线零网络请求由现有浏览器脚本实际操作验证。新增单次点击请求计数用于防止重复事件绑定。

## 最终验证

最低 Go 1.26.0 的终审修正后 `make verify web-test package-unit-test` 已通过，包含普通/race 真实内核、浏览器 14/14、前端专项、BFCache 与制包单测 10/10，见 `final/minimum-reviewed.log`。日常 Go 1.27.3-X:nodwarf5 首轮完整验证通过；终审修正后的完整回归与真实目录包验证继续收尾。

最低 Go 首轮在 `TestW2ActualRunAndOfflineHistory/0` 出现任务成功而引擎退出 255、CLI 返回 125 的失败，原始 `final/minimum.log` 保留。当前代码和批准基线 `fea20fd` 对应场景分别单独执行 `-count=10` 均通过（`minimum-reproduction.log`、`minimum-baseline-reproduction.log`）；最低 Go 后续全套串行通过。根因未确定，不能断言是重构引入、既有抖动或并行负载所致，也未修改内核实现来掩盖它。

## 独立审查与修正

Sol/max 一次只读整分支审查：0 Critical、1 Important、0 Minor。对照基线检查 CLI/受理/执行/事件持久化；前端 66 个业务函数与 75 条原有事件绑定逐项比较，除状态/回调迁移及未用参数移除外无新增行为差异。

审查复现本轮新增的 ResizeObserver 无条件 pagehide 清理会破坏 BFCache 返回后的自动适应。提交 `e4ae8a0` 在持久文档进入缓存时保留观察器，并增加真实历史恢复测试：同一文档返回后调整窗口，检查实际 SVG 尺寸继续适应画布。`bfcache-red.log` 记录修正前 2 秒超时，`bfcache-green.log` 和 `frontend-reviewed.log` 记录修正后及完整浏览器回归通过。

既有 SSE/计时器 pagehide 行为没有扩展修改；缓存恢复验证限定固定 Chromium，不扩大为其他浏览器的兼容性结论。审查摘要为 `final/review.md`。

## 证据与边界

原始日志、截图、比较结果位于 `.work/readability-refactor/`：`baseline/verify.log`、`cli-{before,after,integration}.log`、`service-{after,integration,race}.log`、`frontend-{first,formatted,final}.log`、`visual-comparison.json`、`final/`。scratch 和构建产物遵循工作区 `.work/` / `.cache/` 约束。

本轮自动浏览器操作不新增用户本人验收记录；已有 W3.5 人工验收保持原有来源。没有自动合并、推送、发布或实现 agent 接入；OBS-01/OBS-02 保留为既有未解决观察。
