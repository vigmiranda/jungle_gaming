# Stress, concorrência e recuperação (etapa 11b)

Espelho operacional da especificação externa `STRESS_TESTS.md`. A bateria
oficial da etapa 11 permanece em [`docs/testing.md`](./testing.md); a carga
formal (diferencial) em [`docs/load-testing.md`](./load-testing.md).

## Topologia

```sh
docker compose --profile stress up -d --build
# APIs: :8081 :8082 :8083  |  LB: :8090  |  Keycloak: :8088
# Ready (Windows): curl.exe -sf http://localhost:8090/health/ready
```

Variáveis do harness (`tests/stress`):

```text
BASE_URL=http://localhost:8090
INSTANCE_URLS=http://localhost:8081,http://localhost:8082,http://localhost:8083
KEYCLOAK_URL=http://localhost:8088
ST02_ROUNDS=20          # use 100 para a meta completa da spec
```

## Comandos

```sh
go test -tags=stress -count=1 -timeout 20m ./tests/stress/...
# ou: make stress

./tests/fault/run_smoke.sh
FAULT_APPLY=1 ./tests/fault/run_smoke.sh
# ou: make fault-tests

./scripts/run-load-test.sh          # Linux/macOS/Git Bash
# Windows: .\scripts\run-load-test.ps1
# ou: make load-test
```

## Matriz ST → evidência

| Cenário | Evidência principal |
| --- | --- |
| ST-01 | `tests/stress` `TestST01_…` + integração 50× |
| ST-02 | `TestST02_…` (HTTP) + `TestST02CompetingBetsRepeatedAgainstPostgres` |
| ST-03 | `TestST03_…` |
| ST-04 | `TestST04_…` + `TestST04HTTPAndSQSBurstConcurrent` |
| ST-05 | `TestHandleWagerMessageRedeliveryDoesNotDoubleDebit` |
| ST-06 | profile stress + reenvio; pendências na E11 |
| ST-07 | **N/A** (ADR-012) |
| ST-08…10 | outbox integration + `tests/fault/kill_publisher.sh` |
| ST-11/12 | `pending_reference_test` / reversal tests |
| ST-13/14 | `tests/fault/pause_postgres.sh` / `pause_sqs.sh` |
| ST-15 | Fx lifecycle tests + SIGTERM nos scripts |
| ST-16 | conflitos de idempotência (integração + use case) |
| ST-17 | `TestST17_…` + Bruno auth |

## Fault injection

Scripts em `tests/fault/` localizam containers pelo nome (`api-a`, `postgres`,
`localstack`). Sempre validam o alvo antes de pausar/encerrar.

```sh
./tests/fault/pause_postgres.sh && ./tests/fault/unpause_postgres.sh
FAULT_APPLY=1 ./tests/fault/run_smoke.sh
```

No Windows use **Git Bash** ou WSL para os `.sh`.

## k6

- Smoke health: `loadtests/progressive-load.js`
- Carga formal (throughput, p50/p95/p99, erros, conflitos, outbox lag):
  ver [`docs/load-testing.md`](./load-testing.md)
