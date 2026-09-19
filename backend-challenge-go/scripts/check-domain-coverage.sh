#!/usr/bin/env bash
# Gate de cobertura (ADR-019).
#
# Domínio e camada de aplicação exigem 100%: é onde vivem as invariantes
# financeiras e as decisões de negócio, sem dependência externa que justifique
# trecho sem teste.
#
# Quando um caminho de erro não é alcançável, a saída é eliminar o ramo — mover
# a sequência para um construtor de domínio ou compartilhar um helper entre os
# chamadores —, nunca afrouxar validação para cobri-lo.
#
# Uso: check-domain-coverage.sh [pacotes] [piso]
set -euo pipefail

PACKAGES="${1:-./internal/domain/...}"
THRESHOLD="${2:-100.0}"
PROFILE="$(mktemp -t coverage-XXXXXX.out)"
trap 'rm -f "$PROFILE"' EXIT

if [ -z "$(go list "$PACKAGES" 2>/dev/null)" ]; then
  echo "Nenhum pacote em $PACKAGES; gate de cobertura ignorado."
  exit 0
fi

go test -coverprofile="$PROFILE" "$PACKAGES"

total="$(go tool cover -func="$PROFILE" | awk '/^total:/ {print $3}')"
echo "Cobertura de $PACKAGES: $total (piso: ${THRESHOLD}%)"

measured="${total%\%}"
if awk -v measured="$measured" -v threshold="$THRESHOLD" 'BEGIN { exit !(measured < threshold) }'; then
  echo
  echo "Piso de cobertura não atingido. Trechos sem cobertura:"
  go tool cover -func="$PROFILE" | grep -v '100.0%$' | grep -v '^total:'
  exit 1
fi
