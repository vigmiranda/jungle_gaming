# Etapa 11b — Stress, concorrência e recuperação

## Objetivo

Fechar as lacunas entre a bateria oficial da etapa 11 e a especificação de
stress (`docs/stress-tests.md`, baseada em `STRESS_TESTS.md`): topologia com ≥3
instâncias HTTP, cenários ST sob autenticação real, fault injection e
orquestração reproduzível — **antes** da documentação final (etapa 12).

Carga progressiva com k6 permanece **opcional** (diferencial / Wave 4).

## Done when

- [x] Compose com perfil `stress`: 3 APIs + balanceador
- [x] Scripts de fault injection (`tests/fault/`)
- [x] Helpers Go (`Eventually`, auth OIDC, cliente HTTP, asserções)
- [x] Testes `-tags=stress` para ST-01/ST-02/ST-03/ST-04/ST-17 via HTTP multi-instância
- [x] Reforço na integração (ST-02 repetido, ST-04 concorrente)
- [x] `make stress`, `make fault-tests`, `make load-test` (load opcional)
- [x] Esqueleto k6 em `loadtests/`
- [x] `docs/testing.md` e `docs/stress-tests.md` atualizados
- [x] Etapa 12 referencia comandos de stress / falha

## Não-aplicável (documentar)

- **ST-07** — aceite assíncrono `PENDING` (ADR-012): N/A; equivalentes = inbox
  reentrega, outbox lease, `PENDING_REFERENCE`.

## Por quê antes da etapa 12

A documentação de entrega deve descrever multi-instância, falhas e comandos
reais. Implementar o harness antes evita README especulativo.
