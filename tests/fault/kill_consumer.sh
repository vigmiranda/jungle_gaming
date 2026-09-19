#!/usr/bin/env bash
# Interrompe o consumidor da instância api-a (SIGTERM no container).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

TARGET="${1:-wagering-api-a-1}"
if ! docker ps --format '{{.Names}}' | grep -qx "$TARGET"; then
  # Nome padrão do Compose v2: <project>-api-a-1
  TARGET="$(docker ps --format '{{.Names}}' | grep -E 'api-a' | head -n1 || true)"
fi
if [[ -z "${TARGET}" ]]; then
  echo "container api-a não encontrado; suba com: docker compose --profile stress up -d" >&2
  exit 1
fi
echo "SIGTERM em $TARGET"
docker kill --signal=SIGTERM "$TARGET"
