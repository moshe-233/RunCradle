#!/usr/bin/env bash
# Development/CI helper only. The runtime never downloads dependencies.
set -euo pipefail
version=v1.122.0
archive_sha=9b6dbc38324c0b0481f1cd1dd828ffdc78117129ec797678f4bf8c4023311281
binary_sha=7127df85c0d0c62f2f79b4d2b3082ca1d85f7c0c74523457617861d32150b0dd
test "$(uname -s)/$(uname -m)" = Linux/x86_64
test "$(id -u)" -ne 0
task_source_root=$(cd "$(dirname "$0")/.." && pwd -P)
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
  "https://github.com/F1bonacc1/process-compose/releases/download/${version}/process-compose_linux_amd64.tar.gz" \
  --output "$stage/archive.tar.gz"
printf '%s  %s\n' "$archive_sha" "$stage/archive.tar.gz" | sha256sum --check --status
tar -xzf "$stage/archive.tar.gz" --no-same-owner -C "$stage" process-compose
printf '%s  %s\n' "$binary_sha" "$stage/process-compose" | sha256sum --check --status
chmod 0700 "$stage/process-compose"
mv -- "$stage/process-compose" "$destination/process-compose"
printf '%s\n' "$destination/process-compose"
