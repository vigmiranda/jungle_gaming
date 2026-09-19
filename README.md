# Wagering API — Processamento distribuído de apostas (Go)

Solução do desafio de backend: API HTTP e consumidor SQS que movimentam
carteiras com idempotência persistente, ledger append-only e recuperação
multi-instância. Stack: **Go**, **Uber Fx**, **chi**, **pgx**, **PostgreSQL**,
**Keycloak**, **LocalStack (SQS)**.

Todo o código da solução vive na **raiz deste repositório**. O módulo Go
permanece `github.com/vigmi/backend-challenge-go` (imports inalterados).

O enunciado original está em [`docs/enunciado.md`](docs/enunciado.md).  
Decisões técnicas: [`ARCHITECTURE.md`](ARCHITECTURE.md).  
Testes: [`docs/testing.md`](docs/testing.md) · Stress: [`docs/stress-tests.md`](docs/stress-tests.md).  
Validação manual: [`bruno/README.md`](bruno/README.md).

## Pré-requisitos

- Go **1.27+** (ver `go.mod`)
- Docker + Docker Compose
- `make` (opcional; os comandos equivalentes estão documentados)
- Node.js 18+ (apenas para `make bruno` / Bruno CLI)
- k6 (opcional, só para `make load-test`)

## Subir o ambiente

Na raiz do repositório:

```sh
cp .env.example .env   # ajuste portas se necessário
docker compose up --build -d
```

O Compose sobe PostgreSQL, Keycloak (realm importado), LocalStack (filas),
aplica migrations (`migrate`) e sobe a API. Aguarde o readiness:

```sh
curl -sf http://localhost:8080/health/ready
```

Portas padrão (host): API `8080`, Keycloak `8088`, Postgres `5432`, LocalStack `4566`.

### Filas (LocalStack)

Provisionadas automaticamente por `deploy/localstack/init-queues.sh`:

| Fila | Uso |
| --- | --- |
| `wager-transactions.fifo` | Entrada de operações (FIFO + DLQ) |
| `wager-transactions-dlq.fifo` | Poison / esgotamento |
| `wagering-integration-events` | Destino da transactional outbox |

### Keycloak / identidades de teste

Realm `wagering` importado de `deploy/keycloak/realm-wagering.json`:

| Client | Secret | Papel |
| --- | --- | --- |
| `internal-service` | `internal-service-secret` | Carteiras / reconciliação |
| `provider-a` | `provider-a-secret` | Operações do provedor A |
| `provider-b` | `provider-b-secret` | Isolamento (provedor B) |

Audience da API: `wagering-api`.

## Variáveis de ambiente

Veja [`.env.example`](.env.example). Destaques:

| Grupo | Variáveis |
| --- | --- |
| HTTP | `HTTP_PORT`, timeouts, `HTTP_SHUTDOWN_TIMEOUT` |
| Postgres | `POSTGRES_DSN`, pool |
| SQS | URLs das filas, consumer/publisher, backoff, `WAGER_ALLOWED_PROVIDERS` |
| OIDC | `OIDC_ISSUER_URL`, `OIDC_JWKS_URL` (opcional), `OIDC_AUDIENCE` |
| Pending reference | `PENDING_REFERENCE_*` |
| Stress (host) | `API_A/B/C_HOST_PORT`, `LB_HOST_PORT` |

Com a API em container e token obtido do host, use `OIDC_ISSUER_URL` com o
hostname externo (`localhost`) e `OIDC_JWKS_URL` com o nome interno
(`http://keycloak:8088/.../certs`) — já configurado no Compose.

## Migrations

Aplicadas pelo serviço `migrate` no Compose (não no boot da API).

```sh
make migrate-up                 # go run ./cmd/migrate -command up
make migrate-down               # reverte 1 passo (STEPS=0 reverte todas)
make migrate-version
```

Arquivos em `migrations/` (`*.up.sql` / `*.down.sql`), embarcados no binário.

## Rodar a API sem Compose (binário local)

Com dependências já no ar (`docker compose up -d postgres keycloak localstack`
+ migrate):

```sh
cp .env.example .env
make migrate-up
make run          # go run ./cmd/api
# ou: make build && ./bin/api
```

## Três instâncias (ADR-020)

```sh
# Recomendado: 3 APIs + nginx na :8090
make stress-up
# curl http://localhost:8090/health/ready
# instâncias diretas: :8081 :8082 :8083

# Alternativa: binários locais
docker compose up -d postgres keycloak localstack migrate
make run-multi    # 8081–8083
make stop-multi
```

## Exemplos autenticados

Token do provedor:

```sh
curl -s -X POST "http://localhost:8088/realms/wagering/protocol/openid-connect/token" \
  -d "grant_type=client_credentials&client_id=provider-a&client_secret=provider-a-secret"
```

Aposta (substitua `TOKEN`, `PLAYER`, `WALLET`):

```sh
curl -s -X POST "http://localhost:8080/wagering/transactions" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: provider-a:bet-1" \
  -H "Content-Type: application/json" \
  -H "X-Correlation-Id: demo-1" \
  -d '{
    "providerId":"provider-a",
    "externalTransactionId":"bet-1",
    "playerId":"'"$PLAYER"'",
    "walletId":"'"$WALLET"'",
    "roundId":"round-1",
    "gameId":"game-1",
    "kind":"BET",
    "money":{"amount":"25.00","currency":"BRL"}
  }'
```

Fluxo completo e asserções: coleção Bruno (`make bruno`).

### Rotas principais

| Método | Caminho | Quem |
| --- | --- | --- |
| `GET` | `/health/live`, `/health/ready` | público |
| `GET` | `/metrics` | público (Prometheus) |
| `POST` | `/wallets` | interno |
| `GET` | `/wallets/{id}`, `.../ledger`, `POST .../reconciliation` | interno |
| `POST` | `/wagering/transactions` | provedor |
| `GET` | `/wagering/transactions/{id}` | interno ou provedor dono |
| `GET` | `/providers/{providerId}/wagering/transactions/{externalId}` | interno ou provedor dono |

## Testes

```sh
go test ./...
go test -race ./...                 # ou: make test-race-docker
go vet ./...
make cover-domain                   # gate 100% domain + application
go test -tags=integration -race -count=1 ./tests/...
make bruno

# Stress (etapa 11b) — ambiente stress no ar
make stress-up && make stress
make fault-tests
make load-test                      # opcional (k6)
```

Detalhes e mapeamento dos cenários: [`docs/testing.md`](docs/testing.md).

## Estrutura

| Caminho | Conteúdo |
| --- | --- |
| `cmd/api`, `cmd/migrate` | Entrada do processo |
| `internal/domain` | Domínio puro |
| `internal/application` | Ports e casos de uso |
| `internal/platform` | HTTP, Postgres, SQS, auth, workers |
| `migrations/` | Schema versionado |
| `tests/integration` | Postgres real (testcontainers) |
| `tests/stress` | HTTP multi-instância (`-tags=stress`) |
| `tests/fault` | Scripts de interrupção Compose |
| `bruno/` | Coleção de validação manual |
| `docs/` | Enunciado, testing, stress |
| `ARCHITECTURE.md` | Decisões técnicas da solução |
| `roadmap/` | Plano de execução e ADRs |

## Limitações conhecidas

- **ST-07** (aceite assíncrono `PENDING`): não aplicável — processamento síncrono
  por padrão (ADR-012). Retomada coberta por inbox, outbox e `PENDING_REFERENCE`.
- Operação principal apenas em **BRL** (o tipo carrega moeda; incompatibilidade é rejeitada).
- Diferenciais não feitos (Wave 4): OpenTelemetry tracing, dashboards Grafana,
  carga progressiva completa com relatório de percentis, partidas dobradas.
- Estado `FAILED` de infra permanente existe no domínio; poison SQS sem linha
  persistida vai à DLQ (ADR-016).
- Assinatura JWT/HMAC no envelope SQS fora do escopo (credencial do broker +
  allow-list de `providerId`).

## Checklist anti-eliminatória

- [x] Auth efetiva (Keycloak) em endpoints de negócio
- [x] Sem `float` em dinheiro (`int64` minor units)
- [x] Sem saldo negativo por concorrência (`FOR UPDATE` + constraints)
- [x] Sem movimentação duplicada (idempotência + inbox)
- [x] Idempotência persistente
- [x] Multi-instância (Compose stress / `run-multi`)
- [x] Outbox após commit (`SKIP LOCKED` + lease)
- [x] Ledger append-only (trigger + testes)
- [x] Testes com Postgres + SQS + IdP reais
- [x] Casos do enunciado cobertos por teste (ADR-019)
