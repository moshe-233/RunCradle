GO ?= go
GOFMT ?= gofmt
export RUNCRADLE_CACHE_DIR ?= $(CURDIR)/.cache
export RUNCRADLE_WORK_DIR ?= $(CURDIR)/.work
export TMPDIR := $(CURDIR)/.work/tmp
export PYTHONPYCACHEPREFIX := $(RUNCRADLE_CACHE_DIR)/python
export npm_config_cache := $(RUNCRADLE_CACHE_DIR)/npm
export GOCACHE ?= $(RUNCRADLE_CACHE_DIR)/go-build
export GOPATH ?= $(RUNCRADLE_CACHE_DIR)/go
export GOMODCACHE ?= $(RUNCRADLE_CACHE_DIR)/go-mod
export GOTOOLCHAIN := local
export RUNCRADLE_PROCESS_COMPOSE ?= $(RUNCRADLE_CACHE_DIR)/process-compose/v1.122.0/process-compose

.PHONY: build test unit-test integration-test race vet fmt-check check-tree deps verify prepare experiment-test web-test prepare-web-test

build: | workspace-dirs
	@mkdir -p "$(RUNCRADLE_WORK_DIR)/bin"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -buildvcs=false -trimpath -o "$(RUNCRADLE_WORK_DIR)/bin/runcradle" ./cmd/runcradle

test: | workspace-dirs
	$(GO) test ./... -count=1

unit-test: | workspace-dirs
	$(GO) test ./internal/... ./cmd/... -count=1

race: | workspace-dirs
	$(GO) test -race ./... -count=1

vet: | workspace-dirs
	$(GO) vet ./...

fmt-check:
	@test -z "$$($(GOFMT) -l cmd internal tests/integration)"

check-tree:
	./scripts/check-tree.sh

deps: | workspace-dirs
	$(GO) mod verify

prepare: | workspace-dirs
	./scripts/prepare-process-compose.sh

integration-test: | workspace-dirs
	bash -n scripts/prepare-process-compose.sh scripts/check-tree.sh
	$(GO) test ./tests/integration -count=1

# Compatibility alias for the existing real-engine regression command.
experiment-test: integration-test

prepare-web-test: | workspace-dirs
	./scripts/prepare-web-test.sh

web-test: build
	node tests/web/workbench-graph-test.cjs
	node tests/web/frontend-test.cjs
	node tests/web/web-test.cjs

verify: check-tree fmt-check vet test race deps build

.PHONY: package package-test package-unit-test
package: | workspace-dirs
	GO="$(GO)" python3 scripts/package-release.py --version "$(VERSION)" $(PACKAGE_FLAGS)

package-unit-test: | workspace-dirs
	python3 -m unittest discover -s scripts -p 'test_package_release.py' -v

package-test: | workspace-dirs
	RUNCRADLE_BUNDLE_TEST=1 $(GO) test ./tests/integration -run '^TestBundled' -count=1 -timeout 180s

.PHONY: workspace-dirs
workspace-dirs:
	@mkdir -p "$(RUNCRADLE_CACHE_DIR)" "$(RUNCRADLE_WORK_DIR)" "$(TMPDIR)"
