// Carga formal de wagering (diferencial do enunciado).
//
// Pré-requisitos:
//   docker compose --profile stress up -d --build
//   curl.exe -sf http://localhost:8090/health/ready
//
// Comando reproduzível:
//   k6 run --summary-trend-stats="avg,min,med,p(90),p(95),p(99),max" \
//     -e BASE_URL=http://localhost:8090 \
//     -e KEYCLOAK_URL=http://localhost:8088 \
//     loadtests/wagering-load.js
//
// Relatório JSON (opcional):
//   k6 run ... --out json=loadtests/results/run.json
//   (ou use o handleSummary → loadtests/results/latest-summary.json)
import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";
import { textSummary } from "https://jslib.k6.io/k6-summary/0.0.4/index.js";

const processed = new Counter("wagering_processed");
const rejected = new Counter("wagering_rejected");
const conflicts = new Counter("wagering_http_conflicts");
const replays = new Counter("wagering_http_replays");
const httpErrors = new Counter("wagering_http_errors");
const betDuration = new Trend("wagering_bet_duration", true);

export const options = {
  summaryTrendStats: ["avg", "min", "med", "p(90)", "p(95)", "p(99)", "max"],
  stages: [
    { duration: "20s", target: 5 },
    { duration: "30s", target: 20 },
    { duration: "30s", target: 40 },
    { duration: "20s", target: 10 },
    { duration: "10s", target: 0 },
  ],
  thresholds: {
    checks: ["rate==1"],
    http_req_failed: ["rate<0.02"],
    wagering_bet_duration: ["p(95)<2000", "p(99)<5000"],
  },
};

const base = __ENV.BASE_URL || "http://localhost:8090";
const keycloak = __ENV.KEYCLOAK_URL || "http://localhost:8088";
const realm = __ENV.REALM || "wagering";

// 422 (rejeição de negócio) e 409 (conflito) entram no mix de carga propositalmente.
http.setResponseCallback(http.expectedStatuses(200, 201, 409, 422));

function token(clientId, clientSecret) {
  const res = http.post(
    `${keycloak}/realms/${realm}/protocol/openid-connect/token`,
    {
      grant_type: "client_credentials",
      client_id: clientId,
      client_secret: clientSecret,
    },
    { tags: { name: "keycloak_token" } }
  );
  check(res, { "token 200": (r) => r.status === 200 });
  if (res.status !== 200) {
    throw new Error(`token falhou para ${clientId}: ${res.status} ${res.body}`);
  }
  return res.json("access_token");
}

function uuidv4() {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export function setup() {
  const internalToken = token("internal-service", "internal-service-secret");
  const providerToken = token("provider-a", "provider-a-secret");
  const playerId = uuidv4();

  const open = http.post(
    `${base}/wallets`,
    JSON.stringify({
      playerId,
      initialBalance: { amount: "100000.00", currency: "BRL" },
    }),
    {
      headers: {
        Authorization: `Bearer ${internalToken}`,
        "Content-Type": "application/json",
        "X-Correlation-Id": `k6-setup-${Date.now()}`,
      },
      tags: { name: "open_wallet" },
    }
  );
  check(open, { "wallet 201": (r) => r.status === 201 });
  if (open.status !== 201) {
    throw new Error(`abrir carteira falhou: ${open.status} ${open.body}`);
  }
  const walletId = open.json("id");
  return { providerToken, playerId, walletId, base, startedAt: Date.now() };
}

function postBet(data, externalId, amount, idemKey) {
  const res = http.post(
    `${data.base}/wagering/transactions`,
    JSON.stringify({
      providerId: "provider-a",
      externalTransactionId: externalId,
      playerId: data.playerId,
      walletId: data.walletId,
      roundId: `k6-round-${externalId}`,
      gameId: "k6-load",
      kind: "BET",
      money: { amount, currency: "BRL" },
    }),
    {
      headers: {
        Authorization: `Bearer ${data.providerToken}`,
        "Content-Type": "application/json",
        "Idempotency-Key": idemKey,
        "X-Correlation-Id": `k6-${__VU}-${__ITER}-${externalId}`,
      },
      tags: { name: "bet" },
    }
  );
  betDuration.add(res.timings.duration);

  if (res.status === 409) {
    conflicts.add(1);
    return res;
  }
  if (res.status >= 500 || res.status === 0) {
    httpErrors.add(1);
    return res;
  }
  if (res.status === 200) {
    try {
      const body = res.json();
      if (body.idempotentReplay === true) {
        replays.add(1);
      } else if (body.status === "PROCESSED") {
        processed.add(1);
      } else if (body.status === "REJECTED") {
        rejected.add(1);
      }
    } catch (_) {
      httpErrors.add(1);
    }
    return res;
  }
  if (res.status === 422) {
    rejected.add(1);
    return res;
  }
  httpErrors.add(1);
  return res;
}

export default function (data) {
  const roll = Math.random();
  const uniq = `${__VU}-${__ITER}-${Date.now()}`;

  if (roll < 0.65) {
    // Caminho feliz: BET única de 1.00
    postBet(data, `bet-${uniq}`, "1.00", `provider-a:bet-${uniq}`);
  } else if (roll < 0.8) {
    // Replay idempotente: mesma chave duas vezes na iteração
    const ext = `replay-${__VU}-${Math.floor(__ITER / 5)}`;
    const key = `provider-a:${ext}`;
    postBet(data, ext, "1.00", key);
    postBet(data, ext, "1.00", key);
  } else if (roll < 0.9) {
    // Conflito de payload: mesma Idempotency-Key, amount diferente → 409
    const ext = `conflict-${__VU}-${Math.floor(__ITER / 3)}`;
    const key = `provider-a:${ext}`;
    postBet(data, ext, "1.00", key);
    postBet(data, ext, "2.00", key);
  } else {
    // Pressão de saldo / rejeição: débito grande
    postBet(data, `heavy-${uniq}`, "500.00", `provider-a:heavy-${uniq}`);
  }

  sleep(0.05);
}

export function handleSummary(data) {
  const bet = data.metrics.wagering_bet_duration || {};
  const values = bet.values || {};
  const report = {
    generatedAt: new Date().toISOString(),
    environment: {
      baseUrl: base,
      keycloakUrl: keycloak,
      topology: "compose profile stress (LB :8090, 3 APIs)",
    },
    methodology: {
      tool: "k6",
      script: "loadtests/wagering-load.js",
      stages: options.stages,
      mix: "65% BET 1.00, 15% replay, 10% conflito de payload (409), 10% BET 500.00",
      walletFunding: "100000.00 BRL no setup",
    },
    throughput: {
      http_reqs: data.metrics.http_reqs ? data.metrics.http_reqs.values.count : null,
      http_reqs_rate: data.metrics.http_reqs ? data.metrics.http_reqs.values.rate : null,
      iterations: data.metrics.iterations ? data.metrics.iterations.values.count : null,
    },
    latency_ms: {
      avg: values.avg,
      med_p50: values.med,
      p90: values["p(90)"],
      p95: values["p(95)"],
      p99: values["p(99)"],
      max: values.max,
    },
    errors: {
      http_req_failed_rate: data.metrics.http_req_failed
        ? data.metrics.http_req_failed.values.rate
        : null,
      wagering_http_errors: data.metrics.wagering_http_errors
        ? data.metrics.wagering_http_errors.values.count
        : 0,
    },
    outcomes: {
      processed: data.metrics.wagering_processed
        ? data.metrics.wagering_processed.values.count
        : 0,
      rejected: data.metrics.wagering_rejected
        ? data.metrics.wagering_rejected.values.count
        : 0,
      http_conflicts_409: data.metrics.wagering_http_conflicts
        ? data.metrics.wagering_http_conflicts.values.count
        : 0,
      http_replays: data.metrics.wagering_http_replays
        ? data.metrics.wagering_http_replays.values.count
        : 0,
    },
    outbox_lag: {
      note: "Preenchido pelo wrapper scripts/run-load-test (scrape GET /metrics após o k6).",
    },
  };

  const md = [
    "# Relatório de carga (k6)",
    "",
    `Gerado em: ${report.generatedAt}`,
    "",
    "## Ambiente",
    "",
    `- BASE_URL: \`${report.environment.baseUrl}\``,
    `- KEYCLOAK_URL: \`${report.environment.keycloakUrl}\``,
    `- Topologia: ${report.environment.topology}`,
    "",
    "## Metodologia",
    "",
    `- Script: \`${report.methodology.script}\``,
    `- Stages: ${JSON.stringify(report.methodology.stages)}`,
    `- Mix: ${report.methodology.mix}`,
    `- Funding: ${report.methodology.walletFunding}`,
    "",
    "## Throughput",
    "",
    `- http_reqs: ${report.throughput.http_reqs}`,
    `- http_reqs/s: ${report.throughput.http_reqs_rate}`,
    `- iterations: ${report.throughput.iterations}`,
    "",
    "## Latência BET (ms)",
    "",
    `| p50 (med) | p90 | p95 | p99 | avg | max |`,
    `| --- | --- | --- | --- | --- | --- |`,
    `| ${fmt(report.latency_ms.med_p50)} | ${fmt(report.latency_ms.p90)} | ${fmt(report.latency_ms.p95)} | ${fmt(report.latency_ms.p99)} | ${fmt(report.latency_ms.avg)} | ${fmt(report.latency_ms.max)} |`,
    "",
    "## Erros e conflitos",
    "",
    `- http_req_failed rate: ${fmt(report.errors.http_req_failed_rate)}`,
    `- erros HTTP (contador): ${report.errors.wagering_http_errors}`,
    `- PROCESSED: ${report.outcomes.processed}`,
    `- REJECTED: ${report.outcomes.rejected}`,
    `- HTTP 409: ${report.outcomes.http_conflicts_409}`,
    `- replays idempotentes: ${report.outcomes.http_replays}`,
    "",
    "## Atraso da outbox",
    "",
    "_Seção preenchida por `scripts/run-load-test` após scrape de `/metrics`._",
    "",
    "<!-- OUTBOX_LAG -->",
    "",
  ].join("\n");

  return {
    stdout: textSummary(data, { indent: " ", enableColors: true }),
    "loadtests/results/latest-summary.json": JSON.stringify(report, null, 2),
    "loadtests/results/latest-summary.md": md,
  };
}

function fmt(v) {
  if (v === null || v === undefined || Number.isNaN(v)) return "n/a";
  if (typeof v === "number") return v.toFixed(3);
  return String(v);
}
