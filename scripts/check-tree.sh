#!/usr/bin/env bash
# Fail if tracked or non-ignored untracked files fall outside the product allowlist.
set -euo pipefail
cd "$(dirname "$0")/.."

allow='^(cmd|internal|tests|examples|packaging|third_party|scripts|docs|reference|\.github)/|^(go\.mod|go\.sum|Makefile|README\.md|AGENTS\.md|LICENSE|THIRD_PARTY_NOTICES\.md|\.gitignore|\.gitattributes)$'
bad=$(git ls-files --cached --others --exclude-standard | grep -Ev "$allow" || true)
if [[ -n $bad ]]; then
  echo "check-tree: non-product files found in repository root:" >&2
  echo "$bad" | head -20 >&2
  echo "next: clean up scratch files or update scripts/check-tree.sh deliberately." >&2
  exit 1
fi
