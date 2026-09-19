# Etapa 1 — Esqueleto e infra

## Objetivo

Ter um processo Go que sobe e desce de forma observável, com dependências reais no Docker Compose, **antes** de implementar regras financeiras.

## Done when

- [x] `go.mod` / `go.sum` com versão de Go declarada
- [x] Dockerfile alinhado à versão do `go.mod`
- [x] Docker Compose: PostgreSQL, Keycloak, LocalStack
- [x] Uber Fx com composição inicial + `fx.Lifecycle`
- [x] Roteador `chi` com middleware de correlação (ADR-015, ADR-017)
- [x] `GET /health/live` e `GET /health/ready` com probes reais de Postgres e SQS
- [x] `.env.example` sem segredos reais
- [x] `docker compose up --build` validado ponta a ponta, com readiness real de Postgres e SQS
- [x] Runbook multi-instância: 3 processos locais com portas distintas contra o mesmo Compose (ADR-020)
- [x] Teste de composição Fx: start/stop sem vazar recurso
- [x] `go test -race` executado (em container Go: a máquina Windows não tem toolchain C)

## Notas de execução

- Coleção Bruno em `bruno/` para validação manual; roda também em linha de comando com `make bruno`. Cresce junto com as etapas: as requisições de carteira, operações e autorização já estão escritas com o contrato do desafio.
- Portas do host são parametrizáveis (`POSTGRES_HOST_PORT`, `KEYCLOAK_HOST_PORT`, `LOCALSTACK_HOST_PORT`, `API_HOST_PORT`) para conviver com outros projetos na mesma máquina.
- `-race` roda via `make test-race-docker` onde não houver compilador C.
- O `iss` emitido pelo Keycloak usa o hostname externo; `OIDC_JWKS_URL` permite buscar as chaves pelo nome interno do serviço dentro da rede do Compose.

## Entregáveis

### Estrutura inicial sugerida

```
cmd/api/main.go
internal/config/
internal/platform/postgres/
internal/platform/sqs/
internal/platform/http/
internal/app/           # módulos Fx
migrations/
deploy/docker/          # ou raiz: Dockerfile, compose
.env.example
```

A organização exata é livre; o domínio deve permanecer independente dos adapters.

### Módulos Fx previstos

| Módulo | Responsabilidade |
| --- | --- |
| `config` | Env validado no start; falha rápida se inválido |
| `postgres` | Pool `pgx`, health de readiness |
| `sqs` | Cliente, filas, health de readiness |
| `idp` | JWKS / validação OIDC (pode ser stub nesta etapa) |
| `repositories` | Ports implementados (etapa 4) |
| `usecases` | Casos de uso (etapa 5) |
| `http` | Servidor + handlers |
| `workers` | Consumer SQS, outbox, pending-reference |

### Ciclo de vida (`fx.Lifecycle`)

**Start**

1. Validar configuração
2. Abrir conexões e validar dependências críticas
3. Subir HTTP e workers

**Stop (SIGTERM)**

1. Parar de aceitar novas entradas HTTP
2. Parar long-poll do SQS
3. Concluir ou liberar trabalho em andamento (visibility)
4. Fechar conexões **depois** dos componentes que as usam

### Execução multi-instância (ADR-020)

Evidência exigida pelo desafio: pelo menos três processos com conexões e memória próprias.

- Dependências sobem uma única vez via Compose (Postgres, Keycloak, LocalStack).
- Três processos da aplicação em portas distintas (`HTTP_PORT=8081/8082/8083`), cada um com seu pool `pgx` e seus workers.
- Script/Make target documentado para subir, matar e reiniciar uma instância — usado nos cenários 5, 6 e 8 da etapa 11.
- Alternativa equivalente a documentar: `docker compose up --scale api=3`.

Preparar isso agora evita improviso na etapa 11.

## Por quê esta abordagem

- Os testes de integração exigem **containers reais**. Sem Compose desde o dia 1, cada etapa seguinte vira “funciona na minha máquina”.
- Fx desde o início evita um `main` monólito no final e demonstra o requisito obrigatório de composição.
- Health separado (`live` vs `ready`) prepara orquestração e o item 9 do desafio.

## Fora desta etapa

- Domínio financeiro
- Migrations completas (só bootstrap se necessário)
- Auth real (esqueleto OK)
- Workers de negócio
