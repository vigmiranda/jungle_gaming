# Testes

Como preparar dependências, rodar a suíte e interpretar a bateria obrigatória
(etapa 11 / ADR-019 / ADR-020), a wave de stress (etapa 11b) e o diferencial de
carga.

- Stress / fault: [`docs/stress-tests.md`](./stress-tests.md)
- Carga (k6): [`docs/load-testing.md`](./load-testing.md)

## Roteiro do avaliador (checkout limpo)

Ordem sugerida para reproduzir o que o CI e a validação manual cobrem. Todos os
comandos partem da **raiz do repositório**.

### 1. Subir o ambiente

```sh
cp .env.example .env
docker compose up --build -d
```

Ready (Linux/macOS: `curl -sf`; Windows: `curl.exe -sf`):

```sh
curl.exe -sf http://localhost:8080/health/ready
```

### 2. Bruno (contratos HTTP + Keycloak)

Sem `make` no Windows:

```powershell
cd bruno
npx --yes @usebruno/cli@latest run "01 - Health" "02 - Auth" "03 - Carteiras" "04 - Operacoes" "05 - Consultas" "06 - Autorizacao" --env Local
```

Se `Abrir carteira` retornar 409, o `playerId` do ambiente Local já existe —
`docker compose down -v` e suba de novo, ou troque o `playerId` em
`bruno/environments/Local.bru`.

Com Make: `make bruno` (no Windows o `run .` do Makefile pode listar 0
requests; prefira as pastas explícitas acima).

### 3. Suíte automatizada (espelho do CI)

```sh
go vet ./...
go test ./...
```

Cobertura 100% (PowerShell: aspas no `-coverprofile` / `-func`):

```powershell
go test "-coverprofile=coverage-domain.out" ./internal/domain/...
go tool cover "-func=coverage-domain.out"
go test "-coverprofile=coverage-app.out" ./internal/application/...
go tool cover "-func=coverage-app.out"
```

Integração (testcontainers; `-race` nativo pode falhar no Windows sem CGO):

```sh
go test -tags=integration -count=1 ./tests/...
# CI Linux: go test -tags=integration -race -count=1 ./tests/...
```

Race (Windows sem toolchain C):

```powershell
docker run --rm -v "${PWD}:/src" -w /src -e CGO_ENABLED=1 golang:1.27 go test -race ./...
```

### 4. Multi-instância, stress e fault

```sh
docker compose --profile stress up -d --build
curl.exe -sf http://localhost:8090/health/ready
go test -tags=stress -count=1 -timeout 20m ./tests/stress/...
```

Fault (Git Bash / WSL):

```bash
./tests/fault/run_smoke.sh
FAULT_APPLY=1 ./tests/fault/run_smoke.sh
```

### 5. Carga (diferencial, opcional)

Ver [`docs/load-testing.md`](./load-testing.md). Atalho Windows:

```powershell
.\scripts\run-load-test.ps1
```

## Comandos (resumo)

```sh
go test ./...
go vet ./...
go test -race ./...                 # ou Docker no Windows
./scripts/check-domain-coverage.sh ./internal/domain/... 100.0
./scripts/check-domain-coverage.sh ./internal/application/... 100.0
go test -tags=integration -race -count=1 ./tests/...
docker compose up -d --build && make bruno   # ou npx nas pastas Bruno
docker compose --profile stress up -d --build
go test -tags=stress -count=1 -timeout 20m ./tests/stress/...
./scripts/run-load-test.sh          # ou scripts/run-load-test.ps1
```

## Cobertura (domínio e aplicação)

Gate CI: **100.0%** em `./internal/domain/...` e `./internal/application/...`.

Última medição na etapa 11: ambos os pacotes em **100.0%** de statements.

## Cenários de concorrência e recuperação (etapa 11)

| # | Cenário | Evidência |
| --- | --- | --- |
| 1 | Mesma aposta 50× em paralelo | `TestSameBetSentFiftyTimesInParallelDebitsOnce` |
| 2 | Duas apostas 80.00 sobre 100.00 | `TestTwoCompetingBetsLeaveOneProcessedAndOneRejected` |
| 3 | Carteiras distintas em paralelo | `TestDistinctWalletsProcessInParallel` |
| 4 | ≥ 3 instâncias independentes | `TestThreeIndependentInstancesShareIdempotency` (+ stress-up) |
| 5 | Kill após commit, antes do delete SQS | `TestHandleWagerMessageRedeliveryDoesNotDoubleDebit` |
| 6 | Dois publishers / lease expirado | `TestOutboxClaimIsExclusiveBetweenPublishers`, `TestOutboxExpiredLeaseIsReclaimedWithStableEventID` |
| 7 | REFUND/ROLLBACK antes da referência | `TestPendingReferenceResolvesWhenBetArrivesLater`, `TestPendingReferenceExpiresAcrossWorkerRestarts` |
| 8 | Restart com pendências preservadas | `TestPendingReferenceExpiresAcrossWorkerRestarts` (ver interpretação) |

Cruzamento HTTP × SQS: `TestHandleWagerMessageSharesIdempotencyWithHTTP` + `TestST04HTTPAndSQSBurstConcurrent`.  
Reconciliação saldo × ledger: `TestReconciliationAgainstPostgres`.  
Fx start/stop: `TestAppStartsAndStopsReleasingResources`.

### Interpretação do reinício (item 8)

Não há aceite assíncrono genérico (ADR-012). A retomada exercitada é a de
`PENDING_REFERENCE`, a reentrega do consumidor SQS (inbox) e a republicação da
outbox após lease expirado. Idempotência e estado financeiro vivem no PostgreSQL.
**ST-07** da especificação de stress é N/A.

## Multi-instância local

```sh
docker compose --profile stress up -d --build
# LB :8090 | APIs :8081 :8082 :8083
```

## Simulação de falha

Scripts em `tests/fault/` (ver `docs/stress-tests.md`):

```sh
./tests/fault/pause_postgres.sh
./tests/fault/unpause_postgres.sh
./tests/fault/pause_sqs.sh
./tests/fault/unpause_sqs.sh
./tests/fault/kill_consumer.sh
./tests/fault/kill_publisher.sh
FAULT_APPLY=1 ./tests/fault/run_smoke.sh
```

## Auth

Credenciais e isolamento de provedor: Bruno + Keycloak no Compose, testes de
router, e `TestST17_AuthorizationUnderConcurrency` (`-tags=stress`).
