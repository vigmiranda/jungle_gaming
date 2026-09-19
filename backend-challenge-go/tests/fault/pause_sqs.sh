#!/usr/bin/env bash
# Pausa o LocalStack/SQS (ST-14). Restaure com unpause_sqs.sh.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
TARGET="$(docker ps --format '{{.Names}}' | grep -E 'localstack' | head -n1 || true)"
if [[ -z "${TARGET}" ]]; then
  echo "container localstack não encontrado" >&2
  exit 1
fi
echo "pause $TARGET"
docker pause "$TARGET"
