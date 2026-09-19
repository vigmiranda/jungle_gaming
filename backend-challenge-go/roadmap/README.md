# Roadmap — Backend Challenge Go

Planejamento do desafio de **processamento distribuído de apostas** para a vaga de líder técnico.

Este roadmap traduz o [`../README.md`](../README.md) em etapas executáveis, com a justificativa de cada abordagem. O objetivo é entregar uma solução que maximize pontuação nos critérios de avaliação e evite os itens eliminatórios.

## Critérios de avaliação (âncora)

| Critério | Pontos | Risco eliminatório |
| --- | ---: | --- |
| Integridade financeira | 20 | `float`, saldo negativo, ledger incompleto |
| Concorrência | 20 | lost update, lock global, dependência de 1 instância |
| Idempotência | 15 | só em memória, replay reaplica débito |
| Mensageria e recuperação | 15 | publish antes do commit, sem inbox/outbox |
| Modelagem + Fx | 10 | domínio acoplado a HTTP/SQS/Fx |
| Testes | 10 | mocks no lugar de Postgres/SQS/IdP |
| Observabilidade | 5 | sem logs/métricas/health |
| Documentação | 5 | sem `ARCHITECTURE.md` reproduzível |

**Princípio-guia:** invariantes no banco e no domínio primeiro; HTTP e SQS depois, compartilhando o mesmo caso de uso.

**Regra de testes (vale para todas as etapas):** nenhuma regra de negócio ou garantia do desafio entra como concluída sem teste automatizado que a prove. Domínio e casos de uso com cobertura de branches completa; adapters cobertos por integração com Postgres, LocalStack e Keycloak reais; `go test -race` obrigatório. Detalhes em [00-visao-e-principios.md](./00-visao-e-principios.md#regra-de-testes) e ADR-019.

**Regra de validação manual (vale para todas as etapas):** toda rota nova entra na coleção Bruno (`bruno/`) junto com a implementação, e `make bruno` precisa passar antes de fechar a etapa. Detalhes em [`bruno/README.md`](../bruno/README.md) e ADR-022.

**Regra de entrega (vale para todas as etapas):** cada etapa nasce em uma branch `etapa/NN-slug` com PR aberto para `main`, e só é mergeada com o CI verde. Detalhes em ADR-023.

## Decisões travadas

Todas as decisões estruturais estão aceitas em [decisoes-arquiteturais.md](./decisoes-arquiteturais.md). Resumo do que condiciona a implementação:

| Tema | Decisão | ADR |
| --- | --- | --- |
| Processamento | Síncrono por padrão; assíncrono só em `PENDING_REFERENCE` | ADR-012 |
| Concorrência | `FOR UPDATE` por carteira; `version` é de domínio, não de controle | ADR-004 |
| Replay | `result_balance_*` + `result_payload` gravados no commit | ADR-014 |
| Idempotência | Uniques por provedor + hash canônico estrito | ADR-005, ADR-021 |
| HTTP | `chi`; `202` só em `PENDING_REFERENCE` | ADR-015, ADR-013 |
| Outbox | Fila SQS de integração; `SKIP LOCKED` + lease com TTL | ADR-006 |
| Referências | Esperar se não terminal; rejeitar se terminal inválida; TTL → `REFERENCE_NOT_FOUND` | ADR-009 |
| Falhas | `REJECTED` negócio, `FAILED` infra permanente + DLQ | ADR-016 |
| Multi-instância | 3 processos locais contra o mesmo Compose de dependências | ADR-020 |
| Testes | 100% dos casos do enunciado; domínio com branches completas | ADR-019 |
| Validação manual | Coleção Bruno versionada; toda rota nova entra nela | ADR-022 |
| Entrega | Uma branch e um PR por etapa; merge só com CI verde | ADR-023 |

## Índice das etapas

| Arquivo | Etapa | Foco |
| --- | --- | --- |
| [00-visao-e-principios.md](./00-visao-e-principios.md) | 0 | Decisões arquiteturais que travam o restante |
| [01-esqueleto-e-infra.md](./01-esqueleto-e-infra.md) | 1 | Go modules, Docker Compose, Uber Fx, health |
| [02-dominio.md](./02-dominio.md) | 2 | Money, Wallet, WagerTransaction, Ledger |
| [03-schema-e-constraints.md](./03-schema-e-constraints.md) | 3 | Migrations e garantias no PostgreSQL |
| [04-repositorios-e-transacoes.md](./04-repositorios-e-transacoes.md) | 4 | pgx, Unit of Work, locks |
| [05-casos-de-uso.md](./05-casos-de-uso.md) | 5 | Núcleo compartilhado HTTP + SQS |
| [06-http-e-auth.md](./06-http-e-auth.md) | 6 | API, OAuth/OIDC, autorização |
| [07-sqs-inbox.md](./07-sqs-inbox.md) | 7 | Consumidor FIFO, inbox, DLQ |
| [08-outbox.md](./08-outbox.md) | 8 | Transactional outbox e publishers |
| [09-referencias-pendentes.md](./09-referencias-pendentes.md) | 9 | Worker de `PENDING_REFERENCE` |
| [10-observabilidade.md](./10-observabilidade.md) | 10 | Logs, métricas, health |
| [11-testes-obrigatorios.md](./11-testes-obrigatorios.md) | 11 | Unitários, integração, concorrência, race |
| [12-documentacao-e-entrega.md](./12-documentacao-e-entrega.md) | 12 | README, ARCHITECTURE, Compose reproduzível |
| [decisoes-arquiteturais.md](./decisoes-arquiteturais.md) | ADR | Decisões consolidadas para o `ARCHITECTURE.md` |

## Entrega em waves

A execução é por **wave de pontuação**, não linear pelas etapas. Cada wave termina com testes verdes e commit reproduzível.

### Wave 1 — não morrer nos eliminatórios e travar 40+ pontos

Fatia vertical completa antes de abrir leque de funcionalidades.

1. E1 — esqueleto, Compose (Postgres + Keycloak + LocalStack), Fx, `chi`, health
2. E2 — `Money`, `Wallet`, `WagerTransaction`, ledger, com testes de domínio completos
3. E3 — schema com uniques de idempotência, `result_balance_*`, `result_payload`, ledger imutável
4. E4 — UoW com `pgx` e `SELECT ... FOR UPDATE`
5. **MVP vertical (E5 + E6 parciais):** `OpenWallet` + `BET` via HTTP autenticado no Keycloak, hash estrito, replay com snapshot
6. Testes: mesma aposta 50× em paralelo, 80+80 sobre 100, `-race`

### Wave 2 — completar o núcleo financeiro e a mensageria

- E5 — `WIN`, `LOSS`, `REFUND`, `ROLLBACK`, conflitos de idempotência, reconciliação
- E6 — demais rotas, isolamento entre provedores, mapeamento completo de status
- E7 — consumidor SQS + inbox + DLQ, cruzamento HTTP × SQS
- E8 — outbox com `SKIP LOCKED` + lease, dois publishers

### Wave 3 — recuperação, diagnóstico e entrega

- E9 — worker de `PENDING_REFERENCE` com backoff e TTL
- E10 — logs, métricas, health real
- E11 — bateria oficial completa, incluindo 3 instâncias (ADR-020) e restart
- E12 — `README.md`, `ARCHITECTURE.md`, `.env.example`, comandos oficiais

### Wave 4 — só com folga real

OpenTelemetry, dashboards, testes de carga, partidas dobradas.

```
Wave 1  E1 ██  E2 ████  E3 ██  E4 ███  MVP ████
Wave 2  E5 ████  E6 ███  E7 ████  E8 ███
Wave 3  E9 ██  E10 ██  E11 ██████  E12 ██
Wave 4  diferenciais (opcional)
```

Testes de integração começam **na Wave 1**, junto com o UoW — não só no fim. A etapa 11 fecha a bateria oficial completa.

## Por que essa ordem

1. **Domínio + schema antes de API** — as garantias vivem no agregado e no Postgres.
2. **Fatia vertical na Wave 1** — o risco não é faltar entidade, é descobrir tarde que UoW, Fx, Keycloak ou Compose não fecham ponta a ponta.
3. **Um caso de uso, dois adapters** — evita duas semânticas financeiras.
4. **Auth junto com HTTP** — endpoint de negócio sem IdP é eliminatório.
5. **Inbox/outbox depois do UoW** — sem transação única, o padrão não fecha.
6. **Worker de referência depois** — depende de estado persistido e da máquina de estados.
7. **Testes de falha por fatia** — cada etapa deixa um teste que trava regressão.

## Como usar este roadmap

1. Criar a branch da etapa: `git switch -c etapa/NN-slug`.
2. Ler a etapa atual e a seção **Por quê**.
3. Implementar o **Done when** da etapa, incluindo os testes e as requisições Bruno correspondentes.
4. Atualizar [decisoes-arquiteturais.md](./decisoes-arquiteturais.md) quando uma escolha for confirmada ou revisada (`aceita` → `implementada`).
5. Abrir o PR para `main`, aguardar o CI e mergear com squash quando verde.
6. No final, promover as decisões para `ARCHITECTURE.md` da solução.
