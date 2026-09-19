# Etapa 7 — Consumidor SQS e inbox

## Objetivo

Consumir `WagerTransactionRequested` com as mesmas garantias do caso de uso HTTP, sob entrega at-least-once.

## Done when

- [ ] Filas `wager-transactions.fifo` e `wager-transactions-dlq.fifo` provisionadas com redrive
- [ ] Worker no Fx com lifecycle de shutdown
- [ ] Inbox `UNIQUE (consumer_name, message_id)` na mesma transação do domínio
- [ ] Delete da mensagem **somente após** commit durável
- [ ] Rejeição de negócio confirmada → delete
- [ ] Falha transitória → retry/backoff; permanente/esgotado → `FAILED` auditável + DLQ (ADR-016)
- [ ] `providerId` do corpo validado contra a política, além da credencial do broker (ADR-008)
- [ ] `MessageGroupId` e `MessageDeduplicationId` documentados
- [ ] Teste: kill após commit e antes do delete → reentrega sem segundo débito
- [ ] Teste cruzado HTTP + SQS na mesma operação

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
2. Validar envelope, hash e `providerId` autorizado
3. Abrir UoW: inbox + ProcessWagerTransaction + outbox
4. Commit
5. Delete da mensagem
6. Em erro transitório: não delete; respeitar visibility timeout + backoff
7. Em SIGTERM: parar de buscar; concluir ou devolver visibilidade

### Classificação de falhas (ADR-016)

| Situação | Estado da transação | Mensagem |
| --- | --- | --- |
| Regra de negócio violada | `REJECTED` + `failureCode` | delete (terminal) |
| Infra transitória (Postgres/SQS indisponível) | sem mudança | não delete; retry com backoff |
| Infra permanente ou tentativas esgotadas | `FAILED` (auditoria) | DLQ |
| Envelope inválido / poison sem linha persistida | não persiste | DLQ |

`providerId` desconhecido ou fora da política é recusado **sem efeito financeiro**; tratar como entrada inválida, não como falha de infra.

## Configurações a documentar

- Visibility timeout
- Max receive count → DLQ
- Limites de tentativas da aplicação
- Tratamento de mensagem inválida (poison message)
- `MessageGroupId` recomendado: `walletId` (ordem por carteira)
- `MessageDeduplicationId`: complementar; **não** substitui inbox/idempotência de domínio

## Por quê esta abordagem

- 15 pontos de mensageria e recuperação.
- Delete antes do commit perde o efeito ou cria janela inconsistente; delete sem inbox duplica efeito na reentrega.
- FIFO ajuda ordenação/dedupe do broker, mas o enunciado exige que a aplicação não dependa disso para integridade financeira.

## Referências pendentes neste canal

Se a operação ficar `PENDING_REFERENCE`, a mensagem de entrada pode ser concluída (inbox + delete) **depois** da pendência persistida; o worker da etapa 9 assume a continuidade.
