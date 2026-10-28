# 浏览器 HTTP 样例

需要 python3。prepare 写入 payload；api 监听动态 loopback 端口；check 通过真实 HTTP 检查后返回 EXIT_CODE，写入 checked 和 observed.json。三个节点由 Process Compose 调度。

源码根目录执行 `make build prepare`，再执行：

```bash
./bin/runcradle serve --workspace examples/w3-http --process-compose "$PWD/.cache/process-compose/v1.122.0/process-compose"
```

打开首条 JSON 的 pairing_url。在页面填写 process-compose.yaml 与 ui.json，点击导入预览、保存，选择 check 并运行。默认实际退出 7。

修改元数据原文中 exit_code 的 default 为 0，保存新版本后再运行，应显示实际退出 0。再次打开库内工作流可复用。也可以保持原版本，选择 success 预设；历史分别保留原 revision、预设和解析后的值。显式值覆盖预设，因此切换预设前取消不需要的覆盖勾选。

保存只写 .runcradle/ 私有库，源 YAML／JSON 字节不变。ZIP 导出只含配置和 ui.json，需另行复制本目录 workflow.py。样例业务产物保留在当前目录供查看；不要并发运行这个共享同名产物的样例。
