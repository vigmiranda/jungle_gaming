#!/usr/bin/env bash
# Gate de cobertura (ADR-019).
#
# O domínio exige 100%: é onde vivem as invariantes financeiras e não há
# dependência externa que justifique trecho sem teste.
#
# A camada de aplicação usa um piso pouco abaixo de 100%. O que resta são
# propagações de erro de transições de domínio que o estado validado não
# consegue disparar — por exemplo, concluir uma transação recém-criada. As
# alcançáveis por chamada direta já têm teste white-box; as demais permanecem
# como defesa contra refatoração.
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
