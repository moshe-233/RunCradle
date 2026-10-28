# AGENTS.md — RunCradle Engineering Contract

## 1. Project Overview & Architecture Baseline
RunCradle is a deterministic CLI tool and WebUI workbench (`module runcradle`) written in Go, wrapping **Process Compose (`v1.122.0`)**. It turns process orchestration into a spatial, zero-JSON interactive workbench and CLI while strictly isolating configuration state, observed process execution, engine exit codes, and scoped cleanup.

### Directory Topography (Standard Go Layout)
* `cmd/runcradle/`: Main application entry point.
* `internal/`: Private Go packages strictly isolated by the compiler.
  * `cli/`: CLI commands and flags parsing.
  * `engine/`: Execution engine interface and state machine.
  * `httpapi/`: REST API handlers and route definitions.
  * `nodeexec/`: Native execution and process tree monitoring.
  * `processcompose/`: Process Compose bridge and CLI adapter.
  * `service/`: Core workflow execution orchestrator.
  * `webui/`: Embedded WebUI assets and HTTP handlers.
  * `workflow/`: Core domain types, YAML parser, and schema validations.
* `docs/`: Technical specification (single source of truth).
  * `ARCHITECTURE.md`: Engine contracts, state machine transitions, and package dataflow.
  * `DEVELOPMENT.md`: Build steps, dependency setup, and test execution guide.
  * `USER_GUIDE.md`: CLI arguments, exit codes, and HTTP API endpoints.
  * `WORKFLOW_FORMAT.md`: Schema, UI annotations, and revision algorithms.
  * `archive/`: Historical records and deprecated workspace migrations.
* `tests/`: End-to-end integration tests (`tests/integration/`) and web tests (`tests/web/`).
* `examples/`: Canonical sample workflow configurations.

---

## 2. Developer Operational Commands
All operations are self-contained and reproducible via `Makefile`:

```bash
# 1. Static checks, linting, and formatting check
make fmt-check vet check-tree

# 2. Run unit tests
make unit-test

# 3. Build executable binary into .work/bin/runcradle
make build

# 4. Run full verification suite (fmt, vet, unit, build)
make verify

# 5. Run real engine integration tests
make integration-test
```

---

## 3. Engineering Invariants & Code Standards
1. **Explicit Composition**: `cmd/runcradle/main.go` explicitly initializes dependencies; no magic auto-registration.
2. **Defensive Boundaries**: External input (YAML/JSON from HTTP or CLI) must be validated at the boundary (`internal/workflow`) before reaching services.
3. **Hermetic Builds**: Build artifacts and cache are strictly scoped to `.work/` and `.cache/` (git-ignored); never pollute the workspace root.
4. **Single Source of Truth**: When modifying CLI arguments, schemas, or engine contracts, update the matching documentation in `docs/` within the same commit.
