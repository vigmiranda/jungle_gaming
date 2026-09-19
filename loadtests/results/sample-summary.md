# Relatório de carga (k6) — sample

Exemplo gerado com `scripts/run-load-test.ps1` contra o profile Compose
`stress`. Regenerar localmente com o mesmo comando (artefatos em
`loadtests/results/latest-summary.*` são gitignored).

Gerado em: 2026-09-19T11:11:35.442Z

## Ambiente

- BASE_URL: `http://localhost:8090`
- KEYCLOAK_URL: `http://localhost:8088`
- Topologia: compose profile stress (LB :8090, 3 APIs)

## Metodologia

- Script: `loadtests/wagering-load.js`
- Stages: [{"duration":"20s","target":5},{"duration":"30s","target":20},{"duration":"30s","target":40},{"duration":"20s","target":10},{"duration":"10s","target":0}]
- Mix: 65% BET 1.00, 15% replay, 10% conflito de payload (409), 10% BET 500.00
- Funding: 100000.00 BRL no setup

## Throughput

- http_reqs: 6213
- http_reqs/s: 56.396933823701694
- iterations: 4976

## Latência BET (ms)

| p50 (med) | p90 | p95 | p99 | avg | max |
| --- | --- | --- | --- | --- | --- |
| 218.219 | 559.761 | 667.440 | 958.793 | 260.229 | 1345.060 |

## Erros e conflitos

- http_req_failed rate: 0.000
- erros HTTP (contador): 0
- PROCESSED: 2196
- REJECTED: 2560
- HTTP 409: 1180
- replays idempotentes: 274

## Atraso da outbox

_Seção preenchida por `scripts/run-load-test` após scrape de `/metrics`._

<!-- OUTBOX_LAG -->
- scrape: `http://localhost:8090/metrics`
- amostras (`_count`): 3466
- avg lag (s): 0.298558
- p95 approx (s, bucket `le`): 1
- Prometheus `wagering_concurrency_conflicts_total`: 380
