# Testes

Como preparar dependências, rodar a suíte e interpretar a bateria obrigatória
(etapa 11 / ADR-019 / ADR-020).

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

# Integração com PostgreSQL real (testcontainers)
go test -tags=integration -race -count=1 ./tests/...

# Auth real (Keycloak) + contratos HTTP — Compose + coleção Bruno
docker compose up -d --build
make bruno
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

## Cenários de concorrência e recuperação

| # | Cenário | Evidência |
| --- | --- | --- |
| 1 | Mesma aposta 50× em paralelo | `TestSameBetSentFiftyTimesInParallelDebitsOnce` |
| 2 | Duas apostas 80.00 sobre 100.00 | `TestTwoCompetingBetsLeaveOneProcessedAndOneRejected` |
| 3 | Carteiras distintas em paralelo | `TestDistinctWalletsProcessInParallel` |
| 4 | ≥ 3 instâncias independentes | `TestThreeIndependentInstancesShareIdempotency` (+ `make run-multi`) |
| 5 | Kill após commit, antes do delete SQS | `TestHandleWagerMessageRedeliveryDoesNotDoubleDebit` |
| 6 | Dois publishers / lease expirado | `TestOutboxClaimIsExclusiveBetweenPublishers`, `TestOutboxExpiredLeaseIsReclaimedWithStableEventID` |
| 7 | REFUND/ROLLBACK antes da referência | `TestPendingReferenceResolvesWhenBetArrivesLater`, `TestPendingReferenceExpiresAcrossWorkerRestarts` |
| 8 | Restart com pendências preservadas | `TestPendingReferenceExpiresAcrossWorkerRestarts` (ver interpretação) |

Cruzamento HTTP × SQS: `TestHandleWagerMessageSharesIdempotencyWithHTTP`.  
Reconciliação saldo × ledger: `TestReconciliationAgainstPostgres`.  
Fx start/stop: `TestAppStartsAndStopsReleasingResources`.

### Interpretação do reinício (item 8)

Não há aceite assíncrono genérico (ADR-012). A retomada exercitada é a de
`PENDING_REFERENCE`, a reentrega do consumidor SQS (inbox) e a republicação da
outbox após lease expirado. Idempotência e estado financeiro vivem no PostgreSQL.

## Multi-instância local

Três processos contra o mesmo Compose de dependências (ADR-020):

```sh
docker compose up -d postgres keycloak localstack migrate
make run-multi    # portas 8081, 8082, 8083
make stop-multi
```

No Windows sem `make`, compile `go build -o bin/api.exe ./cmd/api` e suba três
processos com `HTTP_PORT=8081|8082|8083` apontando para o mesmo `.env`.

## Simulação de falha

- **Reentrega SQS:** processar a mensagem duas vezes no handler (teste de
  integração); ou derrubar o processo após o commit e antes do delete.
- **Lease da outbox:** reivindicar com TTL curto e avançar o relógio / esperar
  expiração; outro publisher reassume com o mesmo `eventId`.
- **Poison → DLQ:** envelope inválido ou `providerId` fora da allow-list
  (`isPoison`); Bruno e o job de Compose cobrem auth HTTP com Keycloak real.
- **Cursor inválido do ledger:** `GET .../ledger?cursor=%%%` → 400.

## Auth

Credenciais e isolamento de provedor são exercitados pela coleção Bruno contra
Keycloak no Compose (`02 - Auth`, `06 - Autorizacao`) e pelos testes de router
com identidade injetada. O CI sobe o IdP real no job de integração.
