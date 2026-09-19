# Convenções do projeto

Solução do desafio de processamento distribuído de apostas. As decisões
arquiteturais estão em [`roadmap/decisoes-arquiteturais.md`](./roadmap/decisoes-arquiteturais.md)
e valem como contrato: consulte antes de propor alternativa.

## Fluxo de entrega

Cada etapa do roadmap é uma branch e um PR (ADR-023).

```sh
git switch -c etapa/03-schema-e-constraints   # ao iniciar a etapa
# ... implementação, commits ...
gh pr create --base main --title "Etapa 03 — Schema e constraints"
# CI verde → merge com squash
```

O merge em `main` só acontece com o CI verde. O workflow está em
`.github/workflows/ci.yml`, na raiz do repositório, com três jobs: build e
análise estática, testes com `-race` mais o gate de cobertura, e integração
subindo o Compose para rodar a coleção Bruno.

Ao concluir uma etapa que entrega rotas novas, inclua a pasta correspondente da
coleção na lista do job de integração — ele executa apenas as pastas já
implementadas, porque as demais respondem 404 por enquanto.

## Regras de Done

Uma etapa do roadmap só fecha quando as três regras abaixo valem.

1. **Testes provam a regra** (ADR-019). Nenhuma regra de negócio ou garantia do
   desafio entra como concluída sem teste automatizado que falhe se ela quebrar.
   Domínio e casos de uso têm 100% de cobertura como gate rígido. Adapters são
   cobertos por integração com Postgres, LocalStack e Keycloak reais — mock
   dessas dependências como prova de idempotência, lock ou recuperação é item
   eliminatório no desafio.

2. **Toda rota nova entra na coleção Bruno** (ADR-022). Crie o `.bru` na pasta
   numerada correspondente em `bruno/`, use as variáveis do ambiente `Local` em
   vez de valores fixos, descreva a regra exercitada no bloco `docs` e rode
   `make bruno`. O guia está em [`bruno/README.md`](./bruno/README.md).

3. **Verificação executada:** `gofmt`, `go vet ./...`, `go test ./...` e o
   detector de corridas. Onde não houver toolchain C, use `make test-race-docker`.
   O CI repete tudo isso, então rodar antes evita ciclo de correção no PR.

## Comandos

```sh
docker compose up -d        # ambiente local completo
make check                  # fmt, vet e testes com -race
make test-race-docker       # -race em container, sem toolchain C
make cover-domain           # gate de 100% de cobertura no domínio
make bruno                  # coleção Bruno contra o ambiente local
```

## Estrutura

| Caminho | Conteúdo |
| --- | --- |
| `cmd/api` | Entrada do processo; só compõe o Fx |
| `internal/domain` | Domínio puro, sem Fx, HTTP, SQS ou SQL |
| `internal/platform` | Adapters de infraestrutura e borda HTTP |
| `internal/app` | Composição Fx |
| `bruno/` | Coleção de validação manual |
| `roadmap/` | Plano de execução e ADRs |
| `deploy/` | Realm do Keycloak e provisionamento das filas |

O domínio não importa pacotes de infraestrutura. Fx aparece apenas na borda, em
arquivos `module.go`.
