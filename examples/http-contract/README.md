# 静态 HTTP 契约样例

本目录演示同一个三节点工作流的 YAML 和 JSON 表达，以及可选的 `ui.json` 显示名称、参数和预设。适合验证配置解析和导出的只读依赖图。

在仓库根目录构建后执行：

```bash
make build

./.work/bin/runcradle workflow validate \
  --workspace . --file examples/http-contract/process-compose.yaml \
  --ui examples/http-contract/ui.json

./.work/bin/runcradle workflow render \
  --workspace . --file examples/http-contract/process-compose.yaml \
  --ui examples/http-contract/ui.json --out .work/http-contract.html
```

将 `--file` 的扩展名换成 `.json` 可以校验另一种原生配置格式。这些命令无需启动宿主，不执行业务命令；生成的 HTML 不提供执行控制。若需固定引擎加载检查，先执行 `make prepare`，再为 `validate` 添加 `--check-engine --process-compose "$PWD/.cache/process-compose/v1.122.0/process-compose"`。

配置中的 HTTP 服务使用固定端口 `127.0.0.1:8080`。需要实际运行、动态端口、成功/失败回执和参数复用时，使用 [三节点 HTTP 工作流样例](../w3-http/README.md)。

本目录的 YAML/JSON 还被 [CLI 集成测试](../../tests/integration/cli_test.go) 读取，移动文件时必须同步测试路径。`ui.json` 是可选的展示与参数示例，不是生成物。
