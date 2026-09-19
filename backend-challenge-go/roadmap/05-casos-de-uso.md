# Etapa 5 — Casos de uso (núcleo compartilhado)

## Objetivo

Um único fluxo de aplicação para HTTP e SQS, com as mesmas garantias de idempotência financeira.

**Modelo de processamento (ADR-012):** síncrono. A operação conclui em estado terminal dentro do mesmo request/consumo, exceto quando falta a referência — único caso que sai como `PENDING_REFERENCE` para o worker da etapa 9.

## Done when

- [x] Abrir carteira (com e sem saldo inicial)
- [x] Processar BET / WIN / LOSS / REFUND / ROLLBACK
- [x] Replay idempotente (`idempotentReplay: true`) lendo o resultado persistido
- [x] Conflito: mesma chave, payload diferente
- [x] Conflito: mesmo `(providerId, externalTransactionId)` com outra chave
- [x] Hash canônico estrito, documentado e idêntico em HTTP e SQS
- [x] Reconciliação somente leitura
- [x] `PENDING_REFERENCE` quando a referência ainda não existe
- [x] 100% dos fluxos de decisão acima cobertos por teste (ADR-019)
- [ ] Requisições correspondentes na coleção Bruno — as rotas chegam na etapa 6

## Notas de implementação

| Item | Onde |
| --- | --- |
| Casos de uso | `internal/application/usecase` |
| Hash canônico | SHA-256 sobre JSON com chaves ordenadas, em `idempotency.go` |
| Relógio e identidade | Injetados por port, com implementação em `internal/platform/clock` |

- **A ordem dentro da transação é deliberada:** o lock da carteira vem antes da checagem de idempotência. Duas entregas simultâneas da mesma operação se enfileiram, a segunda encontra o registro da primeira e devolve replay — sem depender de tratar violação de unicidade nem de repetir a transação.
- Rejeições de negócio são **persistidas** como estado terminal, não desfeitas: o reenvio devolve a mesma recusa com o mesmo código.
- Uma operação é revertida no máximo uma vez, por qualquer tipo. Só o índice por tipo deixaria um `REFUND` seguido de `ROLLBACK` devolver o mesmo débito duas vezes (ADR-010).
- A reconciliação roda em transação somente leitura com `REPEATABLE READ`: saldo e ledger precisam vir da mesma visão, ou uma movimentação concorrente apareceria como divergência inexistente.

## Casos de uso

### 1. OpenWallet

- Saldo > 0: cria carteira `version=1`, `OPENING` `PROCESSED`, ledger crédito, outbox `WagerTransactionProcessed` + `WalletBalanceChanged` no mesmo commit.
- Saldo 0: cria carteira sem OPENING/ledger/eventos financeiros.
- Duplicata `(playerId, currency)` → conflito.

### 2. ProcessWagerTransaction

Fluxo lógico:

1. Validar entrada e tipo (rejeitar `OPENING` externo); parsing estrito de `Money`
2. Resolver idempotência (chave + hash + par provider/externalId, tudo no escopo do provedor)
3. Se terminal existente com mesmo hash → replay a partir do resultado persistido
4. Se chave com hash diferente → conflito
5. Lock carteira e aplicar regra de domínio
6. Persistir transação + resultado + ledger (se aplicável) + outbox (+ inbox se SQS)
7. Retornar status, saldo (quando aplicável), `failureCode` se rejeitado

O passo 5 não existe quando a referência é obrigatória e ainda não chegou: nesse caso a operação é persistida como `PENDING_REFERENCE` com o evento correspondente, e o worker da etapa 9 continua.

### 3. ReconcileWallet

- Recalcular saldo a partir do ledger (visão consistente)
- `difference = stored - calculated`
- Não altera saldo
- Reportar divergência em resposta, log e métrica

## Hash de idempotência (ADR-021)

- SHA-256 sobre JSON canônico com chaves ordenadas
- **Excluir** `Idempotency-Key` e metadados de transporte
- Incluir campos de negócio (provider, externalId, player, wallet, round, game, kind, money, reference…)
- **Política estrita:** não há normalização. Formas não canônicas (`"25.0"`, espaços, moeda minúscula, notação científica) são rejeitadas com `INVALID_AMOUNT` antes do hash
- Equivalência HTTP ↔ SQS garantida por construção, já que ambos exigem a mesma forma canônica
- Escopo dos uniques: `(provider_id, idempotency_key)` e `(provider_id, external_transaction_id)`

## Política de reversões (documentar)

- `REFUND` só de `BET` `PROCESSED`
- `ROLLBACK` de `BET` / `WIN` / `REFUND` `PROCESSED`
- Mesma referência: no máximo um REFUND e um ROLLBACK de sucesso; impedir devolução duplicada do mesmo débito
- Concordância: provider, player, wallet, currency, round
- Valor da reversão = valor referenciado (sem parcial)
- Rollback que debitaria além do saldo: `REJECTED` com código **diferente** de `INSUFFICIENT_FUNDS` de BET
- Referência ausente: `PENDING_REFERENCE` + evento correspondente
- Referência existente mas **não terminal** (`PENDING` / `PENDING_REFERENCE`): continuar aguardando, também como `PENDING_REFERENCE` (ADR-009)
- Referência terminal não elegível (`REJECTED`, `FAILED`, tipo incompatível): rejeitar imediatamente com `REFERENCE_NOT_PROCESSED`
- TTL / max attempts: `REJECTED` + `REFERENCE_NOT_FOUND`

## Códigos de falha estáveis (esboço)

| Código | Uso |
| --- | --- |
| `INSUFFICIENT_FUNDS` | BET sem saldo |
| `REFERENCE_NOT_FOUND` | TTL esgotado |
| `REFERENCE_NOT_PROCESSED` | referência terminal não elegível (`REJECTED`/`FAILED`/tipo incompatível) |
| `INVALID_AMOUNT` | LOSS ≠ 0.00, escala inválida, forma não canônica, etc. |
| `CURRENCY_MISMATCH` | moeda incompatível |
| `DUPLICATE_REVERSAL` | segunda reversão do mesmo tipo |
| `REVERSAL_EXCEEDS_BALANCE` | rollback que quebraria saldo ≥ 0 |
| `OPENING_NOT_ALLOWED` | OPENING via canal externo |

## Por quê esta abordagem

- O desafio exige que HTTP e SQS compartilhem o caso de uso.
- Transportes viram adapters finos: menos risco de duas semânticas financeiras.
- Replay com saldo da época é requisito explícito e fácil de errar se o use case misturar “saldo atual” — por isso o resultado vem de coluna persistida, não de recálculo.
- Processamento síncrono mantém a máquina de estados simples: o único estado não terminal observável externamente é `PENDING_REFERENCE`.

## Fora desta etapa

- Middleware JWT (etapa 6)
- Delete da mensagem SQS (etapa 7)
- Publisher da outbox (etapa 8)
- Worker de retry de referência (etapa 9) — aqui só persiste `PENDING_REFERENCE`
