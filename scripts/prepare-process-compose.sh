#!/usr/bin/env bash
# Development/CI helper only. The runtime never downloads dependencies.
set -euo pipefail
task_source_root=$(cd "$(dirname "$0")/.." && pwd -P)
mapfile -t pin < <(python3 - "$task_source_root/internal/processcompose/pinned.json" <<'PY'
import json, sys
p = json.load(open(sys.argv[1]))
for key in ('version', 'archive_sha256', 'binary_sha256', 'archive_url'):
    print(p[key])
PY
)
test "${#pin[@]}" -eq 4
version=${pin[0]}
archive_sha=${pin[1]}
binary_sha=${pin[2]}
archive_url=${pin[3]}
test "$(uname -s)/$(uname -m)" = Linux/x86_64
test "$(id -u)" -ne 0
task_cache="${RUNCRADLE_CACHE_DIR:-$task_source_root/.cache}"
destination="${1:-$task_cache/process-compose/${version}}"
mkdir -p -- "$destination"
destination="$(cd -- "$destination" && pwd -P)"
if test -f "$destination/process-compose" && printf '%s  %s\n' "$binary_sha" "$destination/process-compose" | sha256sum --check --status; then
  printf '%s\n' "$destination/process-compose"
  exit 0
fi
stage="$(mktemp -d "$destination/.prepare.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 \
  --connect-timeout 10 --max-time 120 \
  "$archive_url" \
  --output "$stage/archive.tar.gz"
printf '%s  %s\n' "$archive_sha" "$stage/archive.tar.gz" | sha256sum --check --status
tar -xzf "$stage/archive.tar.gz" --no-same-owner -C "$stage" process-compose
printf '%s  %s\n' "$binary_sha" "$stage/process-compose" | sha256sum --check --status
chmod 0700 "$stage/process-compose"
mv -- "$stage/process-compose" "$destination/process-compose"
printf '%s\n' "$destination/process-compose"
