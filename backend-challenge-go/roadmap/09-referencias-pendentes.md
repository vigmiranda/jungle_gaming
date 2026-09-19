# Etapa 9 — Referências pendentes

## Objetivo

Tratar `REFUND`/`ROLLBACK` (e demais casos) que chegam **antes** da transação referenciada, com retomada durável após restart.

## Done when

- [x] Persistência de `PENDING_REFERENCE` com metadados de retry
- [x] Worker com backoff exponencial
- [x] Max attempts e/ou TTL configuráveis
- [x] Expiração → `REJECTED` + `REFERENCE_NOT_FOUND` + evento de rejeição
- [x] Política da referência aplicada conforme ADR-009 (esperar se não terminal; rejeitar se terminal não elegível)
- [x] Teste: REFUND/ROLLBACK antes da BET → resolve depois ou rejeita por expiração
- [x] Teste: restart com pendências → outra instância retoma

## Calibração (configurável)

| Parâmetro | Default | Env |
| --- | --- | --- |
| Max attempts | 10 | `PENDING_REFERENCE_MAX_ATTEMPTS` |
| TTL | 5m | `PENDING_REFERENCE_TTL` |
| Backoff base | 1s | `PENDING_REFERENCE_BACKOFF_BASE` |
| Backoff max | 30s | `PENDING_REFERENCE_BACKOFF_MAX` |
| Poll | 1s | `PENDING_REFERENCE_POLL_INTERVAL` |
| Batch | 10 | `PENDING_REFERENCE_BATCH_SIZE` |

## Fluxo

1. Caso de uso não resolve referência → status `PENDING_REFERENCE` + outbox `WagerTransactionPendingReference`
2. (SQS) inbox concluída após pendência persistida, se aplicável
3. Worker periodicamente:
   - seleciona elegíveis (`next_retry_at`)
   - tenta resolver referência por `(providerId, referenceExternalTransactionId)`
   - se `PROCESSED` e regras ok → aplica movimento e finaliza `PROCESSED`
   - se ainda ausente → incrementa tentativa, agenda próximo backoff
   - se esgotou → `REJECTED`
4. Toda transição terminal emite o evento correspondente na outbox (mesmo commit)

## Política decidida (ADR-009)

| Situação da referência | Comportamento | Código |
| --- | --- | --- |
| Ausente | Continuar retry com backoff até TTL/max attempts | — |
| `PENDING` / `PENDING_REFERENCE` (não terminal) | Continuar aguardando | — |
| `REJECTED` / `FAILED` | Rejeitar imediatamente | `REFERENCE_NOT_PROCESSED` |
| Tipo incompatível ou divergência de provider/player/wallet/currency/round | Rejeitar imediatamente | `REFERENCE_NOT_PROCESSED` |
| TTL ou max attempts esgotados sem a referência chegar | Rejeitar | `REFERENCE_NOT_FOUND` |
| Referência já revertida, por qualquer tipo | Rejeitar | `DUPLICATE_REVERSAL` |

Rejeitar cedo uma referência ainda em voo quebraria o cenário obrigatório de reversão fora de ordem; esperar sem limite criaria pendência eterna — daí a combinação de espera com TTL/max attempts.

Valores concretos de TTL, max attempts e intervalo de backoff ficam para calibrar nesta etapa, com base no tempo de processamento observado, e precisam ser configuráveis e documentados.

## Por quê esta abordagem

- Cenário obrigatório do enunciado e do teste de recuperação #7.
- Sem worker durável, só “funciona” se os dois eventos forem processados na mesma janela de vida do processo.
- Backoff + persistência prova multi-instância e restart.

## Relação com outras etapas

- Depende da máquina de estados (etapa 2) e UoW (etapa 4)
- Emite outbox (etapa 8)
- Não deve reprocessar a mensagem SQS original se a inbox já foi concluída
