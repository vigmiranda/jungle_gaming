# Testes de carga (diferencial)

Atende o requisito do enunciado para o diferencial de carga: comando
reproduzível, ambiente, metodologia, throughput, p50/p95/p99, erros, conflitos
e atraso da outbox. **Não há meta mínima de RPS.**

## Ambiente

```sh
docker compose --profile stress up -d --build
# Linux/macOS:
curl -sf http://localhost:8090/health/ready
# Windows PowerShell:
curl.exe -sf http://localhost:8090/health/ready
```

| Componente | Endereço |
| --- | --- |
| LB (nginx) | `http://localhost:8090` |
| APIs | `:8081` `:8082` `:8083` |
| Keycloak | `http://localhost:8088` |
| Métricas | `GET /metrics` (Prometheus) |

## Comando reproduzível

Linux / macOS / Git Bash:

```sh
./scripts/run-load-test.sh
# ou: make load-test
```

Windows PowerShell (atualize o PATH se o terminal for anterior à instalação do k6):

```powershell
$env:Path = [System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" +
  [System.Environment]::GetEnvironmentVariable("Path","User")
.\scripts\run-load-test.ps1
```

Equivalente manual:

```sh
k6 run --summary-trend-stats="avg,min,med,p(90),p(95),p(99),max" \
  -e BASE_URL=http://localhost:8090 \
  -e KEYCLOAK_URL=http://localhost:8088 \
  loadtests/wagering-load.js
# depois: scrape GET http://localhost:8090/metrics (wagering_outbox_lag_seconds)
```

Smoke só de health (não cobre o diferencial):

```sh
k6 run loadtests/progressive-load.js
# ou: make load-test-health
```

## Metodologia

Script: [`loadtests/wagering-load.js`](../loadtests/wagering-load.js).

1. **Setup:** obtém tokens Keycloak (`internal-service`, `provider-a`), abre carteira
   com saldo `100000.00` BRL.
2. **Stages:** rampa 5 → 20 → 40 → 10 → 0 VUs (~110s).
3. **Mix por iteração:**
   - 65% BET única `1.00` (throughput)
   - 15% par de BETs com a mesma `Idempotency-Key` (replay)
   - 10% mesma chave com payload diferente (conflito 409)
   - 10% BET `500.00` (pressão de saldo / rejeições 422)
4. **Teardown/relatório:** `handleSummary` grava latência/throughput/erros/conflitos;
   o wrapper faz scrape de `/metrics` e preenche o atraso da outbox.

Status HTTP 200/201/409/422 são tratados como respostas esperadas no k6
(`http.setResponseCallback`), para que rejeições e conflitos do mix não inflacionem
`http_req_failed`.

## O que o relatório cobre

| Exigência | Fonte |
| --- | --- |
| Throughput | `http_reqs`, `http_reqs/s` |
| p50 / p95 / p99 | trend `wagering_bet_duration` (+ summary k6) |
| Erros | `http_req_failed`, contador `wagering_http_errors` |
| Conflitos | HTTP 409 + `wagering_concurrency_conflicts_total` |
| Atraso da outbox | histograma `wagering_outbox_lag_seconds` via `/metrics` |

Artefatos gerados (gitignored, exceto sample):

- `loadtests/results/latest-summary.md`
- `loadtests/results/latest-summary.json`

Exemplo versionado: [`loadtests/results/sample-summary.md`](../loadtests/results/sample-summary.md).

## Interpretação

- Rejeições (`REJECTED` / 422) no mix de BET pesada são **esperadas** sob pressão
  de saldo; não significam falha do teste.
- Conflitos 409 e contador Prometheus refletem corrida/idempotência sob multi-instância.
- Lag da outbox é medido **após** a carga (amostras acumuladas no processo); use o
  LB ou qualquer API do profile stress.
