# Etapa 10 — Observabilidade

## Objetivo

Tornar falhas e duplicatas diagnosticáveis em ambiente multi-instância, sem vazar dados sensíveis.

## Done when

- [x] Logs JSON estruturados
- [x] Campos de correlação quando disponíveis
- [x] Métricas listadas abaixo expostas
- [x] Health `live` / `ready` alinhados à realidade de Postgres e SQS
- [x] Reconciliação reporta divergência em log + métrica
- [x] Política de redaction documentada (sem credenciais, sem payload financeiro completo)

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

### Política de redaction

Implementada em `internal/platform/logging` via `ReplaceAttr` do `slog`:

| Redigido (`[redacted]`) | Mantido |
| --- | --- |
| `authorization`, `*token*`, `*secret*`, `password`, `api_key` | `correlationId`, `messageId`, `transactionId` |
| `money`, `amount`, `payload`, `body`, `initialBalance` | `walletId`, `providerId`, `status`, `failureCode` |

## Métricas (mínimo)

Expostas em `GET /metrics` (Prometheus text).

| Métrica | Uso |
| --- | --- |
| `wagering_transactions_total{channel,status}` | PROCESSED / REJECTED / PENDING_REFERENCE / FAILED |
| `wagering_idempotent_replays_total{channel}` | idempotência |
| `wagering_retries_total{component}` | SQS, outbox, pending_reference |
| `wagering_dlq_messages_total` | veneno / esgotamento |
| `wagering_concurrency_conflicts_total{source}` | lock/version/unique |
| `wagering_outbox_lag_seconds` | lag entre `occurredAt` e publish |
| `wagering_processing_duration_seconds{channel}` | HTTP e consumer |
| `wagering_reconciliation_divergences_total` | alerta de integridade |

## Health

- `GET /health/live` — processo no ar
- `GET /health/ready` — Postgres e SQS alcançáveis

## Correlação SQS

O consumidor lê `correlationId` do envelope; se ausente, reutiliza `messageId`; só gera UUID novo como último recurso (ADR-017).

## Diferenciais (opcional, se houver tempo)

- OpenTelemetry tracing
- Dashboard Grafana/Prometheus
- Não bloquear a entrega principal

## Por quê esta abordagem

- 5 pontos diretos + suporte a demonstrar recuperação nos testes.
- Em sistemas financeiros distribuídos, sem correlação fica impossível provar que um replay não debitou de novo.
