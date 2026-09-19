#!/usr/bin/env bash
# Pausa o PostgreSQL (ST-13). Restaure com unpause_postgres.sh.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

TARGET="$(docker ps --format '{{.Names}}' | grep -E 'postgres' | head -n1 || true)"
if [[ -z "${TARGET}" ]]; then
  echo "container postgres não encontrado" >&2
  exit 1
fi
echo "pause $TARGET"
docker pause "$TARGET"
