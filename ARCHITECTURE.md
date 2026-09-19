# Architecture

Solução de processamento distribuído de apostas. Este documento consolida as
decisões dos ADRs em [`roadmap/decisoes-arquiteturais.md`](roadmap/decisoes-arquiteturais.md).

## Visão geral

Arquitetura **hexagonal**: domínio e casos de uso no centro; HTTP, Postgres,
SQS, Keycloak e workers na borda (`internal/platform`). Composição com **Uber Fx**
(`fx.Module`, `Provide`, `Invoke`, `Lifecycle`). O domínio não importa Fx, HTTP,
SQS nem SQL.

```
HTTP / SQS  →  application/usecase  →  domain
                    ↕ ports
              platform (pgx, SQS, OIDC, workers)
```

Um único fluxo financeiro serve HTTP e SQS (`ProcessWagerTransaction` /
`HandleWagerMessage`).

## Dinheiro (ADR-002, ADR-021)

- `Money` imutável: `int64` em unidades mínimas + `currency` ISO 4217.
- Persistência: `BIGINT` + `CHAR(3)`. Sem `float32`/`float64`.
- Contrato externo: `{"amount":"25.00","currency":"BRL"}`.
- Parsing **estrito**: exatamente duas casas, sem espaços, sem notação científica,
  moeda maiúscula. `"25.0"` → `INVALID_AMOUNT` (não normaliza).
- Overflow tratado no parse e na aritmética; moedas incompatíveis rejeitadas.
- Cenários principais em BRL; o tipo sempre carrega a moeda.

## Transações SQL e repositórios (ADR-003)

- `pgx` + SQL explícito; **Unit of Work** por operação financeira.
- `UnitOfWork.Execute` delimita a `pgx.Tx` compartilhada pelos repositórios.
- Leituras sem movimento usam `ExecuteReadOnly` (reconciliação em
  `REPEATABLE READ`).
- Ports em `internal/application/port`; SQLSTATE `23505`/`23503`/`23514` →
  `port.ErrConflict`; ausência → `port.ErrNotFound`.

## Concorrência (ADR-004)

- Locking pessimista: `SELECT ... FOR UPDATE` na linha da carteira **antes** da
  checagem de idempotência.
- `version`: invariante de domínio (inicia em 1, sobe só com mudança de saldo;
  `LOSS` não incrementa) e campo de `WalletBalanceChanged` — **não** é controle
  otimista.
- Sem lock global; carteiras distintas avançam em paralelo.
- Schema reforça saldo ≥ 0, equação do ledger e uniques (ADR-025).

## Idempotência (ADR-005, ADR-021)

- `UNIQUE (provider_id, idempotency_key)` e
  `UNIQUE (provider_id, external_transaction_id)`.
- Escopo da chave **por provedor**.
- `payload_hash`: SHA-256 de JSON canônico (chaves ordenadas) dos campos de
  negócio; idêntico em HTTP e SQS.
- Replay equivalente → mesmo resultado; mesma chave/payload diferente → 409;
  mesmo `externalTransactionId` com outra chave → 409.
- FIFO do broker **não** é garantia financeira.

## Processamento síncrono (ADR-012, ADR-013)

- Não há aceite assíncrono genérico. `BET`/`WIN`/`LOSS`/`REFUND`/`ROLLBACK`
  concluem no request HTTP ou no consumo SQS.
- Único caminho assíncrono: `PENDING_REFERENCE` (worker dedicado).
- HTTP: `202` **somente** em `PENDING_REFERENCE`; `200` processado; `422` +
  `failureCode` rejeitado; `201` abertura de carteira.

## Replay (ADR-014)

No mesmo commit: `result_balance_*` e `result_payload` (JSONB). Replay devolve o
saldo da época sem reler a carteira nem recalcular pelo ledger.

## Referências pendentes (ADR-009)

- Reversão antes da referência → `PENDING_REFERENCE` + evento correspondente.
- Worker com backoff, max attempts e TTL → `REJECTED` / `REFERENCE_NOT_FOUND`.
- Referência ausente ou não terminal → aguardar; terminal inelegível → rejeitar.

## REFUND × ROLLBACK (ADR-010)

- `REFUND` só de `BET` processada; `ROLLBACK` de `BET`/`WIN`/`REFUND` processados.
- Segunda reversão bem-sucedida da mesma referência (qualquer tipo) →
  `DUPLICATE_REVERSAL` (evita devolver o mesmo débito duas vezes).
- `ROLLBACK` de um `REFUND` (reverter a reversão) continua permitido.

## Inbox e outbox (ADR-006, ADR-008)

- Inbox + efeito financeiro + outbox no **mesmo commit**.
- Delete SQS só após commit; reentrega usa inbox (`consumer`, `message_id`) + hash.
- Outbox: `FOR UPDATE SKIP LOCKED` + lease (`locked_by` / `locked_until`);
  `eventId` estável na republicação.
- Destino: fila SQS de integração no LocalStack (sem SNS).
- Entrada: FIFO `wager-transactions` + DLQ (`maxReceiveCount=5`, visibility 30s);
  `MessageGroupId = walletId`.
- `providerId` fora da allow-list → recusa sem efeito financeiro (DLQ se poison).

## Falhas (ADR-016)

| Classe | Destino |
| --- | --- |
| Negócio | `REJECTED` + `failureCode` (terminal) |
| Infra transitória | retry / visibility / backoff |
| Infra permanente / poison | DLQ; `FAILED` quando houver linha de auditoria |

## Correlação (ADR-017)

- `correlationId` na borda: header `X-Correlation-Id` ou UUID; no SQS do envelope
  (fallback `messageId`).
- Propagado a logs e outbox. `eventId` da outbox nunca regenerado.

## Autenticação (ADR-007)

- Keycloak OIDC; JWT via JWKS; claims `provider_id` / `wallet_role`.
- Interno: carteiras e reconciliação. Provedor: wagering do próprio `providerId`.
- `OIDC_ISSUER_URL` = `iss` externo; `OIDC_JWKS_URL` pode apontar ao hostname
  interno do Compose.

## HTTP (ADR-015)

- `chi` + middlewares: correlação, recover, logging JSON (redaction), auth.
- `/metrics` Prometheus (etapa 10). Health `live` / `ready` (Postgres + SQS).

## Fx e shutdown (ADR-001, ADR-011)

- Módulos por pacote; Lifecycle inicia HTTP e workers; no stop: para poll,
  drena HTTP (`ShutdownTimeout`), libera leases/visibility, fecha pools por último.
- Domínio permanece fora do Fx.

## Testes (ADR-019, ADR-020, etapa 11b)

- Domínio e application: **100%** cobertura (gate CI).
- Integração: testcontainers Postgres; Compose + Bruno (Keycloak/LocalStack).
- Multi-instância: três processos / profile Compose `stress` + LB.
- Stress/fault: `docs/stress-tests.md`. Carga k6 opcional.
- Proibido: mock de Postgres/SQS/IdP como prova de idempotência/lock/recuperação.

## Migrations (ADR-024)

- `golang-migrate` + `embed.FS`; `cmd/migrate`; serviço Compose antes da API.
- Nunca migrar no boot da API (corrida entre instâncias).

## Interpretações adotadas

| Tema | Resolução |
| --- | --- |
| 200 vs 202 | `202` só `PENDING_REFERENCE` |
| Referência não processada | Esperar se não terminal; rejeitar se terminal inválida |
| Outbox destino | SQS integração LocalStack |
| Aceite assíncrono | Não há (ADR-012); ST-07 N/A |
| Idempotency-Key | Por provedor |
| Hash | Estrito, sem normalizar |

## Trabalho não concluído / diferenciais

- OpenTelemetry tracing e dashboards
- Partidas dobradas
- Assinatura criptográfica do envelope SQS
- Multi-moeda operacional além de BRL nos fluxos principais

Carga progressiva com p50/p95/p99, erros, conflitos e atraso da outbox: ver
[`docs/load-testing.md`](docs/load-testing.md) (`scripts/run-load-test`).
