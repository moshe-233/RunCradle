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

最低 Go 1.26.0 与日常 Go 1.27.3-X:nodwarf5 终审修正后的 `make verify web-test package-unit-test` 均已通过，包含普通/race 真实内核、浏览器 14/14、前端专项、BFCache 与制包单测 10/10，见 `final/{minimum,dev}-reviewed.log`。实际归档中主程序的 `go version -m` 也分别确认为上述工具链，证据为 `final/{minimum,dev}-buildinfo.txt`。

### 真实候选目录包

两个候选包均从干净提交 `a9899bf57403cf70064c94b956987502b65cf0b2` 制作，`source_dirty=false`；后续提交仅完善验证文档。未发布远端 release。

| 工具链 | 候选包 | SHA-256 | 真实归档验收 |
| --- | --- | --- | --- |
| Go 1.26.0 | [最低版本包](../../.work/readability-refactor/dist-minimum/runcradle-0.1.0-readability-linux-amd64.tar.gz)，23792977 bytes | `89785477f51a23f5e2f6eb8a1c93bfdc2866c627098b7211550e76da8e4eb9f7` | `make package-test` 通过，15.907s |
| Go 1.27.3-X:nodwarf5 | [日常工具链包](../../.work/readability-refactor/dist-dev/runcradle-0.1.0-readability-linux-amd64.tar.gz)，25069556 bytes | `3336d18faf5f321aa2b8b3fed992eb1d62844c87486dbb27daf24338c907095e` | `make package-test` 通过，16.047s |

归档验收继续覆盖原有 B-01—B-10，并新增从实际包启动后读取在线入口及六个模块的非空 JavaScript/MIME 检查；源码页面通过不再替代包内资源验证。包内工作流执行仍核对真实 0/7、direct_receipt 和限定范围清理。

最低 Go 首轮在 `TestW2ActualRunAndOfflineHistory/0` 出现任务成功而引擎退出 255、CLI 返回 125 的失败，原始 `final/minimum.log` 保留。当前代码和批准基线 `fea20fd` 对应场景分别单独执行 `-count=10` 均通过（`minimum-reproduction.log`、`minimum-baseline-reproduction.log`）；最低 Go 后续全套串行通过。根因未确定，不能断言是重构引入、既有抖动或并行负载所致，也未修改内核实现来掩盖它。

## 独立审查与修正

Sol/max 一次只读整分支审查：0 Critical、1 Important、0 Minor。对照基线检查 CLI/受理/执行/事件持久化；前端 66 个业务函数与 75 条原有事件绑定逐项比较，除状态/回调迁移及未用参数移除外无新增行为差异。

审查复现本轮新增的 ResizeObserver 无条件 pagehide 清理会破坏 BFCache 返回后的自动适应。提交 `e4ae8a0` 在持久文档进入缓存时保留观察器，并增加真实历史恢复测试：同一文档返回后调整窗口，检查实际 SVG 尺寸继续适应画布。`bfcache-red.log` 记录修正前 2 秒超时，`bfcache-green.log` 和 `frontend-reviewed.log` 记录修正后及完整浏览器回归通过。

既有 SSE/计时器 pagehide 行为没有扩展修改；缓存恢复验证限定固定 Chromium，不扩大为其他浏览器的兼容性结论。审查摘要为 `final/review.md`。

## 证据与边界

原始日志、截图、比较结果位于 `.work/readability-refactor/`：`baseline/verify.log`、`cli-{before,after,integration}.log`、`service-{after,integration,race}.log`、`frontend-{first,formatted,final}.log`、`visual-comparison.json`、`final/`。scratch 和构建产物遵循工作区 `.work/` / `.cache/` 约束。

本轮自动浏览器操作不新增用户本人验收记录；已有 W3.5 人工验收保持原有来源。没有自动合并、推送、发布或实现 agent 接入；OBS-01/OBS-02 保留为既有未解决观察。

阅读验收入口为 DEVELOPMENT 的三条路径。中文注释随实现解释资源移交、持久身份、信号/未知事实、原文字节与 int64、草稿缓存及在线/离线边界，不以注释数量作为完成依据。原始执行证据按批准计划保留在 `.work/`，不因技能的通用 scratch 清理步骤删除。
