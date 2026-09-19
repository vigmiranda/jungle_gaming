# Etapa 7 — Consumidor SQS e inbox

## Objetivo

Consumir `WagerTransactionRequested` com as mesmas garantias do caso de uso HTTP, sob entrega at-least-once.

## Done when

- [x] Filas `wager-transactions.fifo` e `wager-transactions-dlq.fifo` provisionadas com redrive
- [x] Worker no Fx com lifecycle de shutdown
- [x] Inbox `UNIQUE (consumer_name, message_id)` na mesma transação do domínio
- [x] Delete da mensagem **somente após** commit durável
- [x] Rejeição de negócio confirmada → delete
- [x] Falha transitória → retry/backoff; permanente/esgotado → DLQ (ADR-016)
- [x] `providerId` do corpo validado contra a política, além da credencial do broker (ADR-008)
- [x] `MessageGroupId` e `MessageDeduplicationId` documentados
- [x] Teste: kill após commit e antes do delete → reentrega sem segundo débito
- [x] Teste cruzado HTTP + SQS na mesma operação

## Envelope de entrada

```json
{
  "messageId": "msg-123",
  "type": "WagerTransactionRequested",
  "occurredAt": "2026-09-08T12:00:00.000Z",
  "data": { "...campos de negócio...", "idempotencyKey": "..." }
}
```

- Identidade durável da inbox: `messageId` do envelope
- Chave de idempotência financeira: `data.idempotencyKey`
- Verificar hash do payload em reentregas

## Fluxo do worker

1. Long-poll / receive
2. Validar envelope, hash e `providerId` autorizado (`WAGER_ALLOWED_PROVIDERS`)
3. Abrir UoW: inbox + `ProcessWagerTransaction.ExecuteIn`
4. Commit
5. Delete da mensagem
6. Em erro transitório: não delete; respeitar visibility timeout
7. Em SIGTERM: parar de buscar; concluir o lote em andamento

### Classificação de falhas (ADR-016)

| Situação | Estado da transação | Mensagem |
| --- | --- | --- |
| Regra de negócio violada | `REJECTED` + `failureCode` | delete (terminal) |
| Infra transitória (Postgres/SQS indisponível) | sem mudança | não delete; retry com backoff |
| Envelope inválido / `providerId` fora da política / hash divergente | não aplica efeito | DLQ + delete |
| Tentativas esgotadas no broker | — | redrive automático (`maxReceiveCount=5`) |

## Configurações

| Parâmetro | Valor inicial | Onde |
| --- | --- | --- |
| Visibility timeout | 30s | LocalStack + `SQS_VISIBILITY_TIMEOUT` |
| Max receive count → DLQ | 5 | `deploy/localstack/init-queues.sh` |
| Long-poll | 20s | `SQS_WAIT_TIME` |
| Lote | até 5 | `SQS_MAX_MESSAGES` |
| Consumidor (inbox) | `wager-consumer` | `SQS_CONSUMER_NAME` |
| Origem autorizada | `provider-a,provider-b` | `WAGER_ALLOWED_PROVIDERS` |
| `MessageGroupId` | `walletId` | produtor (ordem por carteira) |
| `MessageDeduplicationId` | complementar ao broker | **não** substitui inbox/idempotência |

## Por quê esta abordagem

- 15 pontos de mensageria e recuperação.
- Delete antes do commit perde o efeito ou cria janela inconsistente; delete sem inbox duplica efeito na reentrega.
- FIFO ajuda ordenação/dedupe do broker, mas o enunciado exige que a aplicação não dependa disso para integridade financeira.

## Referências pendentes neste canal

Se a operação ficar `PENDING_REFERENCE`, a mensagem de entrada pode ser concluída (inbox + delete) **depois** da pendência persistida; o worker da etapa 9 assume a continuidade.
