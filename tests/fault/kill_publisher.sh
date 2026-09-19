#!/usr/bin/env bash
# Derruba o publisher da instância api-b (container inteiro).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

TARGET="$(docker ps --format '{{.Names}}' | grep -E 'api-b' | head -n1 || true)"
if [[ -z "${TARGET}" ]]; then
  echo "container api-b não encontrado; suba com: docker compose --profile stress up -d" >&2
  exit 1
fi
echo "stop $TARGET"
docker stop "$TARGET"
