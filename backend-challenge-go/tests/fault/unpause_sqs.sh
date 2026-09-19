#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
TARGET="$(docker ps -a --format '{{.Names}}' | grep -E 'localstack' | head -n1 || true)"
if [[ -z "${TARGET}" ]]; then
  echo "container localstack não encontrado" >&2
  exit 1
fi
echo "unpause $TARGET"
docker unpause "$TARGET"
