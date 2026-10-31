# AGENTS.md -- RunCradle Engineering Charter

> **Pragmatic Escape Hatch**: Rules serve delivery, not dogma. Current project constraints and the developer's explicit instruction always take top priority; state any trade-off in one sentence and adapt pragmatically.

## 1. Positioning & Modes (`module runcradle`)
Deterministic Go CLI, loopback WebUI workbench, and Agent-ready tool interface on Linux `amd64` (non-root, Go `>= 1.26.0`, zero runtime LLM/Node.js dependency). Package topography and documentation links are indexed in `README.md`.
- **Mode A -- Wrap (Engine Baseline)**: Pinned **Process Compose `v1.122.0`** is an external binary. Never fork, vendor, or re-implement its scheduler; wrap via input whitelist, isolated temp dir/UDS, `context.Context` deadlines, and `runcradle _exec` (`internal/nodeexec`) exit/signal receipts.
- **Mode B -- Port (Owned Primitives)**: Build small, self-contained mechanisms (~30-100 lines, `Input -> Transform -> Output`) with Go stdlib or vanilla browser APIs. Restrict dependencies to `go.mod` and zero frontend frameworks/bundlers.
- **Mode C -- Skill (Agent Workflow)**: Expose thin CLI subcommands (`internal/cli` -> `internal/hostclient`) + token-budgeted `SKILL.md` playbooks; never embed LLM SDKs or bypass `.runcradle/` APIs.

## 2. Core Engineering Invariants
1. **Standalone-First & Explicit Wiring**: Capabilities must work as standalone terminal commands before Agent integration. Wire dependencies explicitly in `cmd/runcradle/main.go` (sole `os.Exit`, zero `init()` magic).
2. **Model-Friendly ACI**: Validate inputs at boundaries (`internal/workflow`, `DisallowUnknownFields`, `os.Root`). Never prompt on stdin; enforce explicit timeouts. Bound CLI JSON output to `<= 4096 Bytes` (`internal/output`) with pagination, deterministic exit codes (`0` ok, `1` target failed, `2` config/409/429/no-host, `124` timeout, `125` internal/unknown, `128+N` signal), and `next_action`.
3. **Zero Silent Failures & Layered Facts**: Never swallow errors. Keep `state`, `health`, `_exec` `exit_code`/`signal`, `engine_exit_code`, `history_integrity` (`complete|gap|truncated`), and `cleanup` (`complete|unknown`) separate; never guess unknown states or write back to user source files (save strictly to `.runcradle/` CAS with `expected_revision`).
4. **Hermetic Hygiene & Docs Boundary**: Keep builds/caches in `.work/` and `.cache/`. Restrict `cleanup=complete` to `registered_process_groups` and private temp dirs (never kill by historical PID/port/name). Keep `docs/` git-ignored (never `git add -f`); keep `packaging/README.md` free of repo-only relative links.

## 3. Operational Commands (`Makefile`)
```bash
make prepare prepare-web-test            # Pinned engine (v1.122.0) + dev browser tools
make fmt-check vet check-tree unit-test  # Fast static checks + package unit tests
make build integration-test              # Build .work/bin/runcradle + real-engine tests
make verify web-test                     # Full Go gate (incl. -race) + 7 Playwright suites
make package-unit-test && make package VERSION=0.1.0-dev # Offline Linux amd64 bundle
```

## 4. AI Collaboration Rules
1. **Concrete Code Entities Only**: Ban vague buzzwords ("orchestration middleware", "execution orchestrator"). Name exact file paths, function signatures, structs, CLI flags, HTTP routes, and exit codes.
2. **Verifiable Micro-Steps**: Work in 30-60 min increments (~20-50 lines of core logic). State data flow, error branches, and system side effects in 3-5 bullets before multi-file edits; never dump black-box boilerplate.
3. **Guided Debugging & Git Safety**: Trace the 20% main path; debug via *root mechanism -> exact observation point -> minimal fix* so the developer keeps full control. Inspect live Git state first, never auto-commit/push/reset without approval, and keep automated tests distinct from human acceptance.
