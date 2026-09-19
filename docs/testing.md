# Testes

Como preparar dependências, rodar a suíte e interpretar a bateria obrigatória
(etapa 11 / ADR-019 / ADR-020) e a wave de stress (etapa 11b).

Guia de stress / fault / k6: [`docs/stress-tests.md`](./stress-tests.md).

## Comandos

```sh
# Unitários rápidos (domínio, casos de uso, adapters sem containers)
go test ./...
go vet ./...

# Detector de corridas (Linux/macOS com cgo; no Windows use o alvo Docker)
go test -race ./...
# ou: make test-race-docker

# Gate de 100% no domínio e na aplicação
./scripts/check-domain-coverage.sh ./internal/domain/... 100.0
./scripts/check-domain-coverage.sh ./internal/application/... 100.0

# Integração com PostgreSQL real (testcontainers), inclui ST-02 repetido e ST-04 burst
go test -tags=integration -race -count=1 ./tests/...

# Auth real (Keycloak) + contratos HTTP — Compose + coleção Bruno
docker compose up -d --build
make bruno

# Stress HTTP multi-instância (etapa 11b)
make stress-up
make stress
make fault-tests
# make load-test   # opcional, requer k6
```

## Cobertura (domínio e aplicação)

Gate CI: **100.0%** em `./internal/domain/...` e `./internal/application/...`.

Gerar relatório local:

```sh
go test -coverprofile=coverage-domain.out ./internal/domain/...
go tool cover -func=coverage-domain.out
go test -coverprofile=coverage-app.out ./internal/application/...
go tool cover -func=coverage-app.out
```

Última medição na etapa 11: ambos os pacotes em **100.0%** de statements.

## Cenários de concorrência e recuperação (etapa 11)

| # | Cenário | Evidência |
| --- | --- | --- |
| 1 | Mesma aposta 50× em paralelo | `TestSameBetSentFiftyTimesInParallelDebitsOnce` |
| 2 | Duas apostas 80.00 sobre 100.00 | `TestTwoCompetingBetsLeaveOneProcessedAndOneRejected` |
| 3 | Carteiras distintas em paralelo | `TestDistinctWalletsProcessInParallel` |
| 4 | ≥ 3 instâncias independentes | `TestThreeIndependentInstancesShareIdempotency` (+ `make stress-up`) |
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
# Recomendado (3 APIs + nginx LB na :8090)
make stress-up

# Alternativa sem Compose profile
docker compose up -d postgres keycloak localstack migrate
make run-multi    # portas 8081, 8082, 8083
make stop-multi
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
```

## Auth

Credenciais e isolamento de provedor: Bruno + Keycloak no Compose, testes de
router, e `TestST17_AuthorizationUnderConcurrency` (`-tags=stress`).
