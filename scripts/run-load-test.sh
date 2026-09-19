#!/usr/bin/env bash
# Roda a carga formal (k6) e anexa atraso da outbox a partir de GET /metrics.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BASE_URL="${BASE_URL:-http://localhost:8090}"
KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8088}"
mkdir -p loadtests/results

echo "== k6 wagering-load =="
k6 run \
  --summary-trend-stats="avg,min,med,p(90),p(95),p(99),max" \
  -e "BASE_URL=${BASE_URL}" \
  -e "KEYCLOAK_URL=${KEYCLOAK_URL}" \
  loadtests/wagering-load.js

echo "== scrape outbox lag =="
METRICS="$(curl -sf "${BASE_URL}/metrics" || true)"
if [[ -z "${METRICS}" ]]; then
  echo "aviso: não foi possível ler ${BASE_URL}/metrics" >&2
  exit 0
fi

SUM=$(echo "${METRICS}" | awk '/^wagering_outbox_lag_seconds_sum / {print $2; exit}')
COUNT=$(echo "${METRICS}" | awk '/^wagering_outbox_lag_seconds_count / {print $2; exit}')
CONFLICTS=$(echo "${METRICS}" | awk '/^wagering_concurrency_conflicts_total/ {s+=$2} END {print s+0}')
AVG="n/a"
if [[ -n "${COUNT}" && "${COUNT}" != "0" && -n "${SUM}" ]]; then
  AVG=$(awk -v s="${SUM}" -v c="${COUNT}" 'BEGIN {printf "%.6f", s/c}')
fi

P95="n/a"
if [[ -n "${COUNT}" && "${COUNT}" != "0" ]]; then
  TARGET=$(awk -v c="${COUNT}" 'BEGIN {printf "%.6f", c*0.95}')
  P95=$(echo "${METRICS}" | awk -v target="${TARGET}" '
    match($0, /wagering_outbox_lag_seconds_bucket\{le="([^"]+)"\}/, m) {
      if (m[1] == "+Inf") next
      if ($NF+0 >= target+0) { print m[1]; exit }
    }')
  [[ -z "${P95}" ]] && P95="n/a"
fi

BLOCK=$(cat <<EOF
- scrape: \`${BASE_URL}/metrics\`
- amostras (\`_count\`): ${COUNT:-0}
- avg lag (s): ${AVG}
- p95 approx (s, bucket \`le\`): ${P95}
- Prometheus \`wagering_concurrency_conflicts_total\`: ${CONFLICTS}
EOF
)

SUMMARY_MD="loadtests/results/latest-summary.md"
SUMMARY_JSON="loadtests/results/latest-summary.json"
if [[ -f "${SUMMARY_MD}" ]]; then
  # shellcheck disable=SC2001
  TMP="$(mktemp)"
  awk -v block="${BLOCK}" '
    /<!-- OUTBOX_LAG -->/ { print; print block; next }
    { print }
  ' "${SUMMARY_MD}" > "${TMP}"
  mv "${TMP}" "${SUMMARY_MD}"
fi

if [[ -f "${SUMMARY_JSON}" ]] && command -v python3 >/dev/null 2>&1; then
  AVG_JSON="${AVG}"
  [[ "${AVG_JSON}" == "n/a" ]] && AVG_JSON="null"
  P95_JSON="${P95}"
  [[ "${P95_JSON}" == "n/a" ]] && P95_JSON="null"
  COUNT_JSON="${COUNT:-0}"
  python3 - "${SUMMARY_JSON}" "${COUNT_JSON}" "${AVG_JSON}" "${P95_JSON}" "${CONFLICTS}" <<'PY'
import json, sys
path, count, avg, p95, conflicts = sys.argv[1:6]
with open(path, encoding="utf-8") as f:
    data = json.load(f)
def num(x):
    if x in ("null", "n/a", ""):
        return None
    return float(x)
data["outbox_lag"] = {
    "samples": float(count),
    "avg_seconds": num(avg),
    "p95_approx_seconds": num(p95),
    "source": "GET /metrics → wagering_outbox_lag_seconds",
}
data.setdefault("outcomes", {})["prometheus_concurrency_conflicts_total"] = float(conflicts)
with open(path, "w", encoding="utf-8") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PY
fi

echo "Relatório: loadtests/results/latest-summary.md"
