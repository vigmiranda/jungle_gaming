#!/usr/bin/env bash
# Gate de cobertura do domínio (ADR-019).
#
# Domínio e casos de uso exigem 100% de cobertura: abaixo disso a etapa não
# fecha. Adapters ficam fora deste gate — são cobertos por integração com
# infraestrutura real, não por percentual de linha.
set -euo pipefail

PACKAGES="${1:-./internal/domain/...}"
PROFILE="$(mktemp -t coverage-XXXXXX.out)"
trap 'rm -f "$PROFILE"' EXIT

if [ -z "$(go list "$PACKAGES" 2>/dev/null)" ]; then
  echo "Nenhum pacote em $PACKAGES; gate de cobertura ignorado."
  exit 0
fi

go test -coverprofile="$PROFILE" "$PACKAGES"

total="$(go tool cover -func="$PROFILE" | awk '/^total:/ {print $3}')"
echo "Cobertura do domínio: $total"

if [ "$total" != "100.0%" ]; then
  echo
  echo "Gate de 100% não atingido. Trechos sem cobertura:"
  go tool cover -func="$PROFILE" | grep -v '100.0%$' | grep -v '^total:'
  exit 1
fi
