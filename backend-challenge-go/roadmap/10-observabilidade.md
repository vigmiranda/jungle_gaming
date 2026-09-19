# Etapa 10 — Observabilidade

## Objetivo

Tornar falhas e duplicatas diagnosticáveis em ambiente multi-instância, sem vazar dados sensíveis.

## Done when

- [ ] Logs JSON estruturados
- [ ] Campos de correlação quando disponíveis
- [ ] Métricas listadas abaixo expostas
- [ ] Health `live` / `ready` alinhados à realidade de Postgres e SQS
- [ ] Reconciliação reporta divergência em log + métrica
- [ ] Política de redaction documentada (sem credenciais, sem payload financeiro completo)

## Logs

`correlationId` é gerado ou recebido na borda e propagado até a outbox (ADR-017) — o mesmo identificador aparece em log, evento e replay.

Incluir quando existirem:

- `correlationId`
- `messageId`
- `transactionId`
- `walletId`
- `providerId`
- `status` / `failureCode`
- `idempotentReplay`

**Não** registrar: tokens, secrets, corpo financeiro completo da aposta.

## Métricas (mínimo)

| Métrica | Uso |
| --- | --- |
| Resultados por status | PROCESSED / REJECTED / PENDING_REFERENCE / FAILED — `REJECTED` (negócio) e `FAILED` (infra) contados separadamente (ADR-016) |
| Duplicatas / replays | idempotência |
| Retries | SQS, outbox, pending-reference |
| DLQ count | veneno / esgotamento |
| Conflitos de concorrência | lock/version/unique |
| Atraso da outbox | lag entre `occurredAt` e publish |
| Latência de processamento | HTTP e consumer |
| Divergências de reconciliação | alerta de integridade |

## Health

- `GET /health/live` — processo no ar
- `GET /health/ready` — Postgres e SQS alcançáveis

## Diferenciais (opcional, se houver tempo)

- OpenTelemetry tracing
- Dashboard Grafana/Prometheus
- Não bloquear a entrega principal

## Por quê esta abordagem

- 5 pontos diretos + suporte a demonstrar recuperação nos testes.
- Em sistemas financeiros distribuídos, sem correlação fica impossível provar que um replay não debitou de novo.
