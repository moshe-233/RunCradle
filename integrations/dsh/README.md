# RunCradle DSH 插件入口

`dsh-runcradle@0.1.0-alpha.0` 通过一个 Bundle 加载 `runcradle-host`，提供诊断工具 `runcradle_probe` 和可选的 `runcradle-entry` Skill。

当前兼容目标为 Linux、Node 26.12.1、DSH 0.2.2-rc.1，开发包管理器为 pnpm 12.9.1。Desktop、PTC 模式及其他 DSH 版本不在本轮兼容声明内。

## 安装预构建包

```sh
dsh plugin --profile YOUR_PROFILE add /absolute/path/dsh-runcradle-0.1.0-alpha.0.tgz
dsh --profile YOUR_PROFILE --dump-config
dsh --profile YOUR_PROFILE
```

Profile 中的插件行 id 为 `runcradle-host`。安装包包含编译产物，不需要在安装时运行构建脚本。

调用 `runcradle_probe({})` 返回插件名、实际版本和空 echo；调用 `runcradle_probe({echo:"你好"})` 原样返回该字符串。返回值只证明插件入口调用链可用，不代表工作流运行时健康。错误参数、取消和工具限制沿用 DSH 的标准处理。

## 可选 Skill

Profile 的 `cordis.patch.yml` 可加入：

```yaml
- id: runcradle-host
  config:
    enableSkill: false
```

默认 `enableSkill: true`。缺少 `skills` 服务时探针仍可加载；服务恢复后 Skill 自动注册。Skill 包依赖本身仍是 peer dependency。

启用 HMR 时，配置变更会重组插件；未启用 HMR 时，需要重启目标 Profile 后生效。禁用整行可使用 `disabled: true`。卸载：

```sh
dsh plugin --profile YOUR_PROFILE remove dsh-runcradle
```

## 开发与验证

在本包目录执行：

```sh
pnpm install --frozen-lockfile
pnpm run typecheck
pnpm run test
pnpm run test:package
pnpm run test:smoke
pnpm pack --pack-destination /absolute/path/artifacts
```

`build` 使用 tsc；测试使用真实 Cordis、DSH 工具和 Skill 注册表。`test:smoke` 要求 PATH 中存在与 `engines.dsh` 一致的 DSH 0.2.2-rc.1 和 pnpm 12.9.1；它创建独立 DSH_HOME/Profile，并安装和卸载生成的 tgz，不使用日常 Profile。测试启动的实例由脚本负责退出。

DSH 0.2.2 将工具定义和工具执行器分开装配。独立包测试挂载开发依赖 `@deepseek-ai/dsh-tool-executor-native`，CLI 验收使用标准 Profile 提供的 native 执行器。插件 Host 只注册工具，执行器由宿主选择。测试驱动使用新版 `ctx.tools.execute({ calls, signal, agent? })` 批量协议，并检查结果数量和调用 ID 对应关系。

`RUNCRADLE_DSH_TEST_DIR` 可指定测试证据的父目录；默认在系统临时目录创建唯一目录。日志、verification.json、tarball 和 SHA256SUMS 保留供复查。只有包测试借用开发依赖；CLI 测试使用实际安装结果。Agent 测试通过公开 API 创建真实作用域并分发工具，不调用外部模型，属于自动运行时验收。

当前不提供工作流管理、运行/停止、结果查询、CLI runner、工作区服务管理、业务审批或 WebUI；加载和调用探针不启动 RunCradle、不访问工作区内容。
