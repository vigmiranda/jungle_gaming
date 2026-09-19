#!/usr/bin/env bash
# Smoke dos scripts de fault: localiza alvos e valida existência (sem pausar
# em CI por padrão). Com FAULT_APPLY=1 executa pause/unpause rápidos.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

echo "== fault scripts presentes =="
for script in kill_consumer.sh kill_publisher.sh pause_postgres.sh unpause_postgres.sh pause_sqs.sh unpause_sqs.sh; do
  test -x "tests/fault/$script" || chmod +x "tests/fault/$script"
  echo "  ok $script"
done

if [[ "${FAULT_APPLY:-0}" != "1" ]]; then
  echo "FAULT_APPLY!=1: apenas validação dos scripts. Para exercitar: FAULT_APPLY=1 make fault-tests"
  exit 0
fi

echo "== pause/unpause postgres =="
./tests/fault/pause_postgres.sh
./tests/fault/unpause_postgres.sh
echo "== pause/unpause localstack =="
./tests/fault/pause_sqs.sh
./tests/fault/unpause_sqs.sh
echo "fault-tests ok"
