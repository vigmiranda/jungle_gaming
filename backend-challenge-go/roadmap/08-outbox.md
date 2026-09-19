# Etapa 8 — Transactional outbox

## Objetivo

Publicar eventos de integração **somente depois** do commit que os originou, com recuperação multi-instância.

## Done when

- [x] Registros de outbox gravados no mesmo commit do domínio
- [x] Worker publisher separado (Fx lifecycle)
- [x] Disputa segura entre publishers: `FOR UPDATE SKIP LOCKED` **+** lease com TTL (ADR-006)
- [x] Backoff e recuperação de trabalho abandonado (lease expirado volta a ser elegível)
- [x] `eventId` gerado no insert e estável em republicações (ADR-017)
- [x] `correlationId` propagado da borda até o envelope (ADR-017)
- [x] Destino provisionado: fila SQS de integração no LocalStack
- [x] Testes: interrupção entre commit e publish; entre publish e mark-published
- [x] Dois publishers disputando a mesma outbox

## Eventos obrigatórios

| Evento | Gatilho |
| --- | --- |
| `WagerTransactionProcessed` | Conclusão bem-sucedida (inclui `LOSS`) |
| `WagerTransactionRejected` | Rejeição definitiva de negócio |
| `WalletBalanceChanged` | Alteração efetiva de saldo |
| `WagerTransactionPendingReference` | Espera por referência |

## Envelope

Campos: `eventId`, `eventType`, `aggregateId`, `correlationId`, `causationId?`, `occurredAt`, `version`, `data` tipado.

- Timestamps UTC RFC 3339
- Money em string decimal canônica
- Payload da outbox = snapshot imutável
- Tipo/versão definidos no construtor do evento

### Identidade e correlação (ADR-017)

| Campo | Origem |
| --- | --- |
| `eventId` | UUID gerado no insert da outbox; **nunca** regenerado em republicação |
| `correlationId` | Gerado ou recebido na borda (header HTTP; no SQS deriva do envelope) e propagado pelo caso de uso |
| `causationId` | Evento ou mensagem que originou a operação, quando houver |
| `aggregateId` | `walletId` ou `transactionId`, conforme o evento |

Não reutilizar `transactionId` como `correlationId`: são papéis distintos (negócio vs rastreio).

### `WalletBalanceChanged.data`

`walletId`, `transactionId`, `direction`, `money`, `balanceBefore`, `balanceAfter`, `walletVersion`.

## Worker publisher

1. Buscar lote de pendentes elegíveis (`next_attempt_at <= now`, `locked_until` nulo ou expirado) com `FOR UPDATE SKIP LOCKED`
2. Claim atômico gravando `locked_by` + `locked_until` (TTL maior que o tempo máximo esperado de publish)
3. Publicar no destino (fila SQS de integração)
4. Marcar `published_at` e liberar o lease (ou atualizar tentativas e `next_attempt_at` em falha)
5. Em crash após publish e antes do mark: o lease expira, outro publisher reassume e republica com **mesmo** `eventId` (consumidores devem ser idempotentes)

`SKIP LOCKED` sozinho não recupera worker morto no meio do publish; o lease com TTL é o que prova "recuperação de trabalho abandonado".

## Por quê esta abordagem

- Garantia obrigatória: “eventos externos só depois da confirmação da transação”.
- Publicar inline no request acopla latência do broker e quebra atomicidade sob falha.
- Lease / `SKIP LOCKED` demonstra que múltiplas instâncias não corrompem a outbox.

## Destino de saída

Fila SQS de integração provisionada no LocalStack (ADR-006 — sem SNS, para manter a stack mínima e testável).

| Item | Valor |
| --- | --- |
| Nome | `wagering-integration-events` |
| Provisionamento | `deploy/localstack/init-queues.sh` (Compose) |
| URL local | `SQS_INTEGRATION_QUEUE_URL` |
| Contrato | envelope JSON com `eventId` estável; consumidor externo idempotente por `eventId` |
| Quem consome | sistemas de integração externos (fora deste serviço) |
| Publisher | worker Fx `awssqs.Publisher` (`SQS_PUBLISHER_*`) |
