# Etapa 3 — Schema e constraints

## Objetivo

Impor invariantes financeiras **no PostgreSQL**, independentemente de locks locais, de memória de processo ou da deduplicação FIFO do SQS.

## Done when

- [x] Migrations versionadas com up **e** down documentados
- [x] Constraints da tabela abaixo aplicadas
- [x] Ledger protegido contra UPDATE/DELETE
- [x] Distinção schema entre origem `INTERNAL` e `EXTERNAL`
- [x] Resultado do processamento persistido para replay (ADR-014)
- [x] Índice que sustenta o cursor opaco do ledger (ADR-018)
- [x] Teste de integração que prova **cada** constraint desta etapa (ex.: saldo negativo rejeitado, ledger imutável, uniques de idempotência)

## Notas de implementação

| Item | Onde |
| --- | --- |
| SQL versionado | `migrations/000001_initial_schema.{up,down}.sql`, embarcado com `embed.FS` |
| Comando | `cmd/migrate` (`up`, `down -steps N`, `version`), exposto no Makefile |
| Execução no Compose | Serviço `migrate` roda antes da API, via `service_completed_successfully` |
| Testes | `tests/integration` com build tag `integration`, PostgreSQL real por testcontainers |

- A moeda da movimentação é garantida por chave estrangeira composta para `wallets (id, currency)`: nenhuma transação ou lançamento pode usar moeda diferente da carteira.
- A imutabilidade do ledger é um gatilho que aborta `UPDATE` e `DELETE` com `restrict_violation`, escolhido em vez de revogação de privilégio porque também vale para o dono da tabela.
- O índice parcial de reversão só considera as processadas, então uma tentativa rejeitada não bloqueia a reversão legítima seguinte.
- Decisões registradas em ADR-024 (migrations) e ADR-025 (invariantes no schema).

## Tabelas principais (mínimo)

- `wallets`
- `wager_transactions`
- `wallet_ledger_entries`
- `inbox_messages`
- `outbox_events`
- (opcional auxiliar) `idempotency_records` se não estiver embutido em `wager_transactions`

## Constraints essenciais

| Invariante | Como impor |
| --- | --- |
| Uma carteira por `(player_id, currency)` | `UNIQUE` |
| Saldo ≥ 0 | `CHECK (balance_minor >= 0)` |
| Ledger imutável | revoke `UPDATE`/`DELETE` ou trigger que aborta |
| 1 lançamento por transação na carteira | `UNIQUE (wallet_id, transaction_id)` |
| Idempotência externa | `UNIQUE (provider_id, external_transaction_id)` **e** `UNIQUE (provider_id, idempotency_key)` — escopo por provedor, nunca global (ADR-005) |
| Inbox | `UNIQUE (consumer_name, message_id)` |
| Sem `OPENING` duplicado | unique parcial / origem INTERNAL vs EXTERNAL |
| Uma reversão do mesmo tipo por referência | unique parcial `(reference_id, kind)` para REFUND/ROLLBACK bem-sucedidos |
| Money | `BIGINT` (centavos) + `CHAR(3)` moeda; nunca float |

## Colunas de apoio à concorrência e recuperação

- `wallets.version` (`BIGINT` / `INT`) — invariante de domínio e campo de evento; o controle de concorrência é o `FOR UPDATE` (ADR-004)
- `wager_transactions.status`, `failure_code`, `payload_hash`, `idempotency_key`
- `wager_transactions.attempt_count`, `next_retry_at` (para `PENDING_REFERENCE`)
- `outbox_events.published_at`, `attempts`, `next_attempt_at`, `locked_until` / `locked_by` (lease — ADR-006)
- `inbox_messages.received_at`, `completed_at`, `payload_hash`

### Resultado persistido para replay (ADR-014)

Gravados no mesmo commit do processamento, na `wager_transactions`:

| Coluna | Uso |
| --- | --- |
| `result_balance_minor` (`BIGINT`, nullable) | Saldo da carteira **no momento do processamento original** |
| `result_balance_currency` (`CHAR(3)`, nullable) | Moeda do saldo acima |
| `result_payload` (`JSONB`) | Snapshot imutável da resposta devolvida ao provedor |

Nulos quando a operação não produz resultado financeiro (ex.: rejeição antes de tocar a carteira). O replay lê essas colunas: **nunca** recalcula o saldo pelo ledger nem lê o saldo atual da carteira.

### Suporte ao cursor do ledger (ADR-018)

- Índice em `wallet_ledger_entries (wallet_id, created_at, id)`
- Cursor opaco em base64 sobre `(created_at, id)`; sem offset

## Por quê esta abordagem

- Item eliminatório: “invariantes financeiras no banco”.
- Constraints transformam bugs de aplicação em erros SQL reproduzíveis nos testes.
- Uniques de idempotência e inbox são a base da sobrevivência a restart total.
- Sem o resultado persistido, o replay tende a devolver o saldo atual — erro clássico e diretamente penalizado.

## Documentar em ARCHITECTURE

- Representação de `Money` no schema
- Estratégia anti-UPDATE no ledger
- Unique parciais escolhidas para REFUND/ROLLBACK
- Escopo por provedor dos uniques de idempotência
- Colunas de resultado usadas no replay
- Como `OPENING` é distinguido de operações externas
