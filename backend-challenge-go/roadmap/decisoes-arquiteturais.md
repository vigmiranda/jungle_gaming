# Decisões arquiteturais (rascunho vivo)

Documento de trabalho para alimentar o `ARCHITECTURE.md` final. Atualizar a cada etapa quando uma escolha for confirmada, revisada ou descartada.

Status: `proposta` → `aceita` → `implementada` → `revisada`.

---

## ADR-001 — Arquitetura hexagonal + Uber Fx

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Domínio deve ser independente de Fx, HTTP, SQS e persistência |
| Decisão | Ports no centro; adapters em `internal/platform` / `internal/adapters`; composição via `fx.Module` |
| Consequências | Testes unitários sem infra; Fx só na borda; mais arquivos, menos acoplamento |

## ADR-002 — Representação de Money

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Proibição de float; contrato externo em string decimal |
| Decisão | `int64` em unidades mínimas (centavos) + `currency` ISO 4217; persistência `BIGINT` + `CHAR(3)` |
| Alternativas | `shopspring/decimal` / `NUMERIC` |
| Consequências | Overflow explícito; escala 2 fixa; BRL nos cenários principais; testes de moeda incompatível |

## ADR-003 — Acesso ao banco

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Preferência do desafio por SQL explícito |
| Status | implementada |
| Decisão | `pgx` + SQL; Unit of Work por operação financeira |
| Delimitação | A transação começa e termina em `UnitOfWork.Execute`; os repositórios daquela chamada compartilham a mesma `pgx.Tx`. Leituras que não movimentam saldo usam `ReadOnly()`, fora de transação |
| Ports | Interfaces em `internal/application/port`, do lado de quem consome; a implementação `pgx` fica confinada à borda |
| Tradução de erros | SQLSTATE `23505`, `23503` e `23514` viram `port.ErrConflict`; ausência de linha vira `port.ErrNotFound`. Os casos de uso não conhecem SQLSTATE |
| Consequências | Locks e commits visíveis; sem GORM; repositórios não abrem commit escondido |

## ADR-004 — Concorrência por carteira

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Teste 80+80 vs 100; paralelismo entre carteiras; multi-instância |
| Decisão | Locking **pessimista puro**: `SELECT ... FOR UPDATE` na linha da carteira. `version` **não** é mecanismo de concorrência |
| Papel da `version` | Invariante de domínio (inicia em `1`, incrementa só com mudança de saldo; `LOSS` não incrementa) e campo de `WalletBalanceChanged` |
| Alternativas descartadas | Otimista puro com retry; híbrido lock + retry por conflito de versão |
| Consequências | Serialização por carteira; sem lock global; carteiras distintas em paralelo; sem política de retry otimista para testar |

## ADR-005 — Idempotência

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | At-least-once HTTP e SQS; restart total; multi-tenant por provedor |
| Decisão | `UNIQUE (provider_id, idempotency_key)` **e** `UNIQUE (provider_id, external_transaction_id)`, com `payload_hash` canônico persistido na transação |
| Escopo da chave | Por provedor, não global — a chave pertence ao cliente e não deve cruzar tenants |
| Consequências | Três casos cobertos: replay equivalente, mesma chave com payload diferente (409), mesmo `externalTransactionId` com outra chave (409). FIFO não é garantia financeira |

## ADR-006 — Inbox + Outbox

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Não perder evento confirmado; não publicar antes do commit |
| Decisão | Inbox e outbox no mesmo commit do domínio; publisher assíncrono com **`FOR UPDATE SKIP LOCKED` + lease com TTL**; `eventId` gerado no insert e estável em republicação |
| Destino | **Fila SQS de integração no LocalStack** (sem SNS), provisionada no Compose |
| Lease | `locked_by` / `locked_until`; TTL maior que o tempo máximo esperado de publish; expirado volta a ser elegível |
| Consequências | Delete SQS só após commit; republicação possível com mesmo `eventId`; consumidores externos devem ser idempotentes |

## ADR-007 — Autenticação e autorização

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | IdP externo obrigatório; isolamento por provedor |
| Decisão | Keycloak no Compose; OIDC JWT validado por JWKS; claim → `providerId`; `client_credentials` para serviço interno |
| Endpoint do JWKS | `OIDC_ISSUER_URL` é o hostname externo (o mesmo do `iss` do token) e `OIDC_JWKS_URL` permite apontar para o nome interno do serviço, já que a API em container não resolve o hostname do host |
| Consequências | Endpoints de negócio autenticados; sem emissão própria de token; realm e clients (`internal-service`, `provider-a`, `provider-b`) importados automaticamente pelo Compose |

## ADR-008 — Mensageria e confiança na origem

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | SQS obrigatório; LocalStack; consumidor não pode confiar cegamente no corpo |
| Decisão | FIFO `wager-transactions` + DLQ com redrive; `MessageGroupId = walletId`; dedupe do broker complementar |
| Controle de origem | Credenciais/políticas do broker **mais** validação de domínio: `providerId` desconhecido ou fora da política é recusado, sem efeito financeiro |
| Consequências | Ordem por carteira; DLQ para poison/permanente; documentar visibility e `maxReceiveCount` |

## ADR-009 — Referências pendentes

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Reversão pode chegar antes da BET |
| Decisão | `PENDING_REFERENCE` + worker com backoff exponencial + TTL/max attempts → `REJECTED` com `REFERENCE_NOT_FOUND` |
| Política da referência | Referência ausente ou **não terminal** (`PENDING` / `PENDING_REFERENCE`) → continuar aguardando; referência terminal não elegível (`REJECTED` / `FAILED` / tipo incompatível) → rejeitar imediatamente |
| Consequências | Continuação após restart; mensagem SQS pode ser concluída após persistir a pendência |

## ADR-010 — REFUND × ROLLBACK

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Evitar devolução duplicada do mesmo débito; códigos de falha distintos |
| Status | implementada |
| Decisão | REFUND só de BET processada; ROLLBACK de BET/WIN/REFUND processados; rejeitar rollback que viole saldo ≥ 0 com código distinto de `INSUFFICIENT_FUNDS` |
| Uma reversão por operação | A aplicação recusa a **segunda reversão de qualquer tipo** sobre a mesma referência, com `DUPLICATE_REVERSAL`. O unique parcial por `(reference, kind)` no schema é a rede de proteção; sozinho ele deixaria passar um `REFUND` seguido de um `ROLLBACK` da mesma aposta, devolvendo o mesmo débito duas vezes |
| Encadeamento válido | Reverter a própria reversão continua permitido: o `ROLLBACK` de um `REFUND` desfaz aquele crédito, e não o débito original |
| Consequências | A checagem ocorre sob o lock da carteira, então duas reversões concorrentes não se cruzam. Combinações documentadas no ARCHITECTURE |

## ADR-011 — Shutdown

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | SIGTERM com trabalho em andamento |
| Decisão | Fx Lifecycle: drain HTTP, parar poll SQS, concluir ou devolver visibility, fechar deps por último |
| Consequências | Reentrega segura; sem half-commit órfão sem inbox/outbox |

## ADR-012 — Modelo de processamento: síncrono por padrão

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | O desafio permite concluir operações sem dependências de forma síncrona, sem commit intermediário de aceite |
| Decisão | **Não há aceite assíncrono** de operação financeira ordinária. `BET`, `WIN`, `LOSS`, `REFUND` e `ROLLBACK` resolvem dentro do request HTTP ou do consumo SQS. O único caminho assíncrono é `PENDING_REFERENCE`, retomado pelo worker de referências |
| Alternativas descartadas | Aceite assíncrono genérico (persistir `PENDING` e processar depois): exigiria claim/lease de transações e testes de kill adicionais, sem ganho na rubrica |
| Consequências | `PENDING` no caminho feliz é transitório dentro da própria transação; o cenário obrigatório de retomada durável é coberto pelo worker de `PENDING_REFERENCE`, pelo consumidor SQS e pelo publisher da outbox. Deve ficar explícito no `ARCHITECTURE.md` |

## ADR-013 — Contrato HTTP em pendência

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Distinguir estado terminal de estado em espera |
| Decisão | `202 Accepted` **apenas** quando o estado resultante for `PENDING_REFERENCE`; estados terminais respondem `200` (`PROCESSED`), `422` (`REJECTED` com `failureCode`), `409` (conflito), `400`, `401`, `403`, `503` conforme a situação; abertura de carteira responde `201` |
| Consequências | Com ADR-012, `202` na prática só aparece em reversão que chegou antes da referência; mapeamento estável e testável |

## ADR-014 — Resultado persistido para replay

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Replay deve devolver o saldo observado no processamento original, mesmo após novas movimentações |
| Decisão | Persistir na `wager_transactions`, no mesmo commit: `result_balance_minor` + `result_balance_currency` **e** `result_payload` (snapshot imutável da resposta ao provedor, em JSONB) |
| Alternativas descartadas | Recalcular pelo ledger no replay (frágil com `LOSS`, rejeições e ordenação) |
| Consequências | Replay é leitura pura; o snapshot garante fidelidade de contrato; colunas nulas para operações sem resultado financeiro |

## ADR-015 — Roteamento HTTP

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Necessidade de middlewares (JWT, correlação, logging) organizados |
| Decisão | `chi` sobre `net/http` |
| Consequências | Handlers finos; middleware de auth e correlação isolado; domínio permanece sem dependência de HTTP |

## ADR-016 — Classificação de falhas: REJECTED × FAILED × DLQ

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | O modelo tem `REJECTED` (negócio) e `FAILED` (infra permanente) |
| Decisão | Regra de negócio violada → `REJECTED` com `failureCode` (terminal, mensagem pode ser removida da fila). Falha transitória de infra → retry/backoff, sem mudar estado. Falha permanente de infra ou tentativas esgotadas → `FAILED` registrado para auditoria **e** mensagem encaminhada à DLQ |
| Consequências | Métricas e contrato não misturam recusa de negócio com defeito de infraestrutura; poison message sem linha persistida vai direto à DLQ |

## ADR-017 — Correlação e identidade de eventos

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Envelope exige `correlationId`; logs precisam rastrear a operação ponta a ponta |
| Decisão | `correlationId` gerado/propagado **na borda** (header HTTP quando presente, senão UUID novo; no SQS deriva do envelope) e repassado ao caso de uso e à outbox. `eventId` é UUID gerado no insert da outbox e nunca regenerado em republicação. `causationId` referencia o evento/mensagem que originou a operação, quando houver |
| Alternativas descartadas | Gerar correlação só no publisher; reutilizar `transactionId` como `correlationId` |
| Consequências | Logs, eventos e replays compartilham o mesmo identificador de rastreio |

## ADR-018 — Paginação do ledger

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Enunciado exige cursor opaco e ordenação estável |
| Decisão | Cursor opaco em base64 sobre `(created_at, id)`, com índice correspondente; sem offset |
| Consequências | Resultado estável sob inserts concorrentes; cursor inválido → 400 |

## ADR-019 — Estratégia de testes

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Testes valem 10 pontos e vários eliminatórios dependem de evidência executável |
| Decisão | **100% dos casos do enunciado** cobertos por teste automatizado. Domínio com **100% de cobertura como gate rígido**; camada de aplicação com piso de 98%. Adapters cobertos por integração com infra real (Postgres, LocalStack, Keycloak), não por cobertura de linha |
| Exceção da aplicação | Os poucos statements restantes são propagação de erro de transições de domínio que o estado já validado não consegue disparar — concluir uma transação recém-criada, por exemplo. As alcançáveis por chamada direta têm teste white-box; as demais ficam como defesa contra refatoração. Afrouxar validação só para cobri-las seria pior que a lacuna |
| Regra de Done | Nenhuma regra de negócio ou garantia entra como concluída sem teste que falhe se ela quebrar |
| Proibições | Asserção apenas de "não deu erro"; mock de Postgres/SQS/IdP como prova de idempotência, lock ou recuperação |
| Fora da meta de linha | `main`, wiring Fx, Dockerfile e clients AWS — validados por teste de composição e integração, não por percentual |
| Consequências | `go test ./...`, `go test -race ./...` e `go vet ./...` a cada fatia; relatório de cobertura do domínio no README |

## ADR-020 — Execução multi-instância

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Evidência exigida com pelo menos três processos independentes |
| Decisão | Três processos locais (`go run` / binário) com portas distintas apontando para o mesmo Compose de dependências; script documentado para subir, matar e reiniciar instâncias |
| Alternativas | `docker compose up --scale api=3` (documentar como equivalente) |
| Consequências | Conexões e memória próprias por processo; facilita kill controlado nos cenários 5, 6 e 8 |

## ADR-021 — Normalização antes do hash de idempotência

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Proibido arredondar silenciosamente; hash deve ser equivalente entre HTTP e SQS |
| Decisão | Política **estrita**: aceitar apenas a forma canônica (`amount` com exatamente duas casas, sem espaços, sem notação científica, `currency` ISO 4217 em maiúsculas). Formas equivalentes como `"25.0"` são rejeitadas com `INVALID_AMOUNT`, não normalizadas |
| Hash | SHA-256 sobre JSON canônico com chaves ordenadas dos campos de negócio, excluindo `Idempotency-Key` e metadados de transporte |
| Consequências | Nenhuma normalização silenciosa a documentar além da rejeição; hash idêntico em HTTP e SQS por construção |

## ADR-022 — Coleção Bruno versionada como entregável

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | A API precisa ser validável manualmente a cada etapa, sem montar requisição na hora, e o avaliador precisa exercitar os fluxos autenticados a partir de um checkout limpo |
| Decisão | Coleção Bruno em `bruno/`, versionada no repositório e aberta com **Open Collection** (não `Import`, que quebra o vínculo com os arquivos). Ambiente `Local` com as credenciais do realm e encadeamento de variáveis por `vars:post-response` |
| Regra de Done | **Toda rota nova entra na coleção junto com a implementação da etapa.** Uma etapa não fecha sem a requisição correspondente e sem `make bruno` verde |
| Alternativas | Postman/Insomnia (formato exportado, pior de versionar); apenas `curl` no README (não encadeia variáveis nem valida asserções) |
| Consequências | A coleção cresce com o projeto e serve como especificação executável; roda em linha de comando via `@usebruno/cli`, com código de saída utilizável em pipeline |

## ADR-023 — Uma branch e um PR por etapa, com CI obrigatório

| Campo | Valor |
| --- | --- |
| Status | aceita |
| Contexto | Entregar o desafio em um único commit esconde o raciocínio e impede revisar incrementos; sem verificação automatizada, o "funciona na minha máquina" só aparece na avaliação |
| Decisão | Cada etapa do roadmap nasce em uma branch `etapa/NN-slug`, com PR aberto para `main`. O merge só acontece com o CI verde |
| Fluxo | Criar a branch ao iniciar a etapa → commits durante a implementação → abrir o PR → CI do GitHub Actions → merge (squash) em `main` ao fechar a etapa |
| CI | Três jobs: build e análise estática (`gofmt`, `go mod verify`, `go vet`, `go build`); testes com `-race` e o gate de 100% no domínio; integração subindo o Compose e executando a coleção Bruno |
| Consequências | Histórico com uma entrega por etapa; regressão barrada antes do merge; o job de integração prova que o ambiente sobe a partir de um checkout limpo, que é o critério de entrega do desafio |

## ADR-024 — Migrations versionadas e embarcadas, aplicadas fora do boot

| Campo | Valor |
| --- | --- |
| Status | implementada |
| Contexto | O desafio exige migrations versionadas com aplicação e reversão documentadas, em ambiente com várias instâncias |
| Decisão | `golang-migrate` com driver `pgx/v5`, arquivos `NNNNNN_nome.{up,down}.sql` embarcados via `embed.FS` e aplicados pelo comando `cmd/migrate` |
| Quando roda | Nunca no start da aplicação. No Compose, um serviço `migrate` roda antes da API (`service_completed_successfully`); em produção seria uma etapa do deploy |
| Alternativas | Migrar no boot da API (vira corrida entre instâncias); migrador próprio (controle total, mas reinventa uma roda madura); arquivos soltos ao lado do binário (a imagem distroless carrega só o executável) |
| Consequências | A mesma imagem entrega API e migrador; o estado sujo é reportado em vez de ignorado; `down` é executável e testado, não apenas documentado |

## ADR-025 — Invariantes financeiras impostas pelo schema

| Campo | Valor |
| --- | --- |
| Status | implementada |
| Contexto | Locks locais e a deduplicação do SQS FIFO não protegem contra bug na aplicação, instância antiga em execução ou escrita manual |
| Decisão | Saldo não negativo, política de valor por tipo, separação entre origem interna e externa, uniques de idempotência por provedor, uma reversão bem-sucedida por tipo e equação do ledger, todos como constraints nomeadas |
| Moeda | Chave estrangeira composta `(wallet_id, currency)` para `wallets (id, currency)`, em transações e lançamentos: o banco garante que toda movimentação usa a moeda da carteira |
| Ledger | Gatilho `BEFORE UPDATE OR DELETE` que aborta com `restrict_violation`, escolhido em vez de revogação de privilégio porque também vale para o dono da tabela |
| Consequências | Bug de aplicação vira erro SQL reproduzível; cada constraint tem nome estável e teste de integração que prova a recusa |

---

## Limitações e trabalho consciente

| Item | Status |
| --- | --- |
| Partidas dobradas | opcional / fora se faltar tempo |
| OpenTelemetry | diferencial |
| Testes de carga | diferencial |
| Assinatura de mensagem SQS (JWT/HMAC no envelope) | fora do escopo; controle por credencial do broker + validação de domínio |
| Multi-moeda operacional | só BRL nos fluxos principais; tipo carrega moeda |

## Interpretações fechadas

| # | Questão | Resolução | ADR |
| --- | --- | --- | --- |
| 1 | HTTP 200 vs 202 em pendência | `202` só em `PENDING_REFERENCE` | ADR-013 |
| 2 | Referência existe mas não está `PROCESSED` | Esperar se não terminal; rejeitar se terminal não elegível | ADR-009 |
| 3 | Destino dos eventos de outbox | Fila SQS de integração no LocalStack | ADR-006 |
| 4 | Claim da outbox | `SKIP LOCKED` + lease com TTL | ADR-006 |
| 5 | Biblioteca HTTP | `chi` | ADR-015 |
| 6 | Aceite assíncrono de operação | Não há; síncrono por padrão | ADR-012 |
| 7 | Escopo da `Idempotency-Key` | Por provedor | ADR-005 |
| 8 | Normalização antes do hash | Estrita, sem normalizar | ADR-021 |

## Interpretações ainda abertas

| # | Questão | Quando decidir |
| --- | --- | --- |
| 1 | Valores concretos de TTL/max attempts da referência, visibility timeout e `maxReceiveCount` | Etapas 7 e 9, com base no tempo de processamento medido |
| 2 | Política de leitura de `GET /wagering/transactions/:transactionId` para client de provedor (permitir só as próprias ou restringir ao serviço interno) | Etapa 6 |
| 3 | Se `wallets`/`ledger` ficam exclusivamente internos ou expostos a provedores com filtro | Etapa 6 |
