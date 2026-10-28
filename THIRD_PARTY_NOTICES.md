# Third-party notices

- gopkg.in/yaml.v3 v3.0.1: native YAML syntax nodes; [license](third_party/licenses/yaml.v3-LICENSE).
- github.com/gorilla/websocket v1.5.3: private upstream state WebSocket client, BSD-2-Clause; [license](third_party/licenses/gorilla-websocket-LICENSE).
- golang.org/x/sys v0.48.0: Linux bounded output file-descriptor operations, BSD-3-Clause; [license](third_party/licenses/golang-x-sys-LICENSE).
- Process Compose v1.122.0: separate validation/execution binary, Apache-2.0, redistributed unchanged in directory bundles as `libexec/process-compose`. [Upstream license](third_party/licenses/process-compose/LICENSE); [exact binary dependency inventory and license provenance](third_party/licenses/process-compose/inventory.json). The inventory covers all 85 dependency modules in the pinned official binary, plus Go runtime license materials. Its scheduler is not copied into this Go module. Product runtime does not download it.

The bounded output sink is adapted from the project's own archived RuntimeHealer implementation under the same Apache-2.0 license. The new repository has no build dependency on that archive.
