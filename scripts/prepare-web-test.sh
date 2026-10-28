#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
task_cache="${RUNCRADLE_CACHE_DIR:-$PWD/.cache}"
export npm_config_cache=${npm_config_cache:-$task_cache/npm}
command -v node >/dev/null
command -v npm >/dev/null
mkdir -p "$task_cache/web-test"
task_modules=${RUNCRADLE_PLAYWRIGHT_MODULE_DIR:-$task_cache/web-test/node_modules}
if ! NODE_PATH="$task_modules" node -e 'if(require("playwright/package.json").version!=="1.62.1")process.exit(1)' 2>/dev/null; then
  npm install --prefix "$task_cache/web-test" --ignore-scripts --no-audit --no-fund --save-exact playwright@1.62.1
  task_modules="$task_cache/web-test/node_modules"
fi
task_browser=${RUNCRADLE_BROWSER:-$task_cache/web-test/chrome/chrome-linux64/chrome}
if [[ ! -x "$task_browser" ]]; then
  if [[ -n ${RUNCRADLE_BROWSER:-} ]]; then
    echo "Configured RUNCRADLE_BROWSER is unavailable: $task_browser" >&2
    exit 2
  fi
  command -v curl >/dev/null
  command -v unzip >/dev/null
  task_temp=$(mktemp -d "$task_cache/web-test/chrome-install-XXXXXXXX")
  trap 'rm -rf -- "$task_temp"' EXIT
  curl --fail --location --max-time 180 --retry 0 --output "$task_temp/chrome.zip" https://storage.googleapis.com/chrome-for-testing-public/155.0.8059.39/linux64/chrome-linux64.zip
  unzip -q "$task_temp/chrome.zip" -d "$task_temp"
  mkdir -p "$task_cache/web-test"/chrome
  mv -T "$task_temp/chrome-linux64" "$task_cache/web-test/chrome/chrome-linux64"
fi
task_version=$("$task_browser" --version)
[[ "$task_version" == *155.0.8059.39* ]] || { echo "Chrome 155.0.8059.39 required, observed $task_version" >&2; exit 2; }
NODE_PATH="$task_modules" node -e 'if(require("playwright/package.json").version!=="1.62.1")throw Error("Playwright 1.62.1 required");console.log("Playwright "+require("playwright/package.json").version)'
printf '%s\n' "$task_version"
sha256sum "$task_browser"
