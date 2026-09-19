# Etapa 0 — Visão e princípios

## Objetivo

Travar as decisões que o desafio exige documentar em `ARCHITECTURE.md` **antes** de espalhar código. Mudar no meio (ex.: `float` → `int64`, GORM → SQL explícito) custa caro.

## Done when

- [x] Decisões abaixo revisadas e aceitas
- [x] Registro em [decisoes-arquiteturais.md](./decisoes-arquiteturais.md) (ADR-001 a ADR-021, status `aceita`)
- [x] Limitações conscientes listadas (o que fica para o final / fora do escopo)
- [x] Regra de testes definida como critério de Done de todas as etapas

## Decisões aceitas

### 1. Arquitetura hexagonal (ports & adapters)

Domínio sem Fx, HTTP, SQS ou `pgx`. Adapters implementam ports.

**Por quê:** o enunciado exige domínio independente de Fx/HTTP/SQS/persistência; vale 10 pontos de modelagem e evita acoplamento que impede testes unitários puros.

### 2. `pgx` + SQL explícito

Preferencial no desafio. Transações, locks e constraints ficam verificáveis.

**Por quê:** GORM esconde a unidade de trabalho; o avaliador quer ver `BEGIN`, `FOR UPDATE` e `COMMIT` delimitados.

### 3. `Money` como `int64` em unidades mínimas (centavos)

Escala fixa de 2 casas, moeda ISO 4217 (BRL nos cenários principais). Overflow tratado em parse/soma/subtração/negação.

**Por quê:** proibição absoluta de `float32`/`float64`; `BIGINT` no banco preserva o valor exatamente. Biblioteca decimal é válida, mas `int64` é mais simples de persistir e de testar limites.

### 4. Lock pessimista puro por carteira (`version` é de domínio)

`SELECT ... FOR UPDATE` na linha da carteira é o **único** mecanismo de concorrência. A coluna `version` não faz controle otimista: ela é invariante de domínio (inicia em `1`, incrementa só com mudança de saldo, `LOSS` não incrementa) e compõe o payload de `WalletBalanceChanged`.

**Por quê:** o teste obrigatório (duas apostas de 80.00 sobre 100.00) exige serialização **por carteira**, sem lock global. Carteiras distintas avançam em paralelo. Misturar lock com retry otimista adicionaria uma política de retry para testar sem ganho de garantia.

### 5. Idempotência no schema, com escopo por provedor

`UNIQUE (provider_id, idempotency_key)` **e** `UNIQUE (provider_id, external_transaction_id)`, mais `payload_hash` canônico persistido.

**Por quê:** deve sobreviver ao reinício de **todos** os processos; deduplicação FIFO do SQS não conta como garantia financeira. O escopo por provedor evita que a chave de um cliente colida com a de outro, e os dois uniques cobrem os três casos exigidos: replay equivalente, mesma chave com payload diferente e mesmo `externalTransactionId` com outra chave.

### 6. Inbox + outbox na mesma transação SQL

Estado da operação, saldo, ledger, inbox e outbox confirmados atomicamente (conforme aplicável).

**Por quê:** eventos externos só depois do commit; reentrega at-least-once não duplica efeito nem perde evento já confirmado.

### 7. Keycloak + OAuth 2.0 / OIDC

`client_credentials` entre serviços; JWT com claim → `providerId` autorizado.

**Por quê:** IdP externo é obrigatório; cadastro de senhas e emissão própria de tokens estão fora do escopo. Autenticação efetiva é **eliminatória**.

### 8. SQS FIFO no LocalStack (ou MiniStack)

Filas `wager-transactions.fifo` + DLQ com redrive. `MessageGroupId = walletId` para ordenação por carteira; `MessageDeduplicationId` complementar, **nunca** substituto da inbox.

**Por quê:** o enunciado assume at-least-once e proíbe depender da deduplicação do broker para integridade financeira.

No consumidor, credencial do broker **não** é suficiente: o `providerId` do corpo é validado contra a política antes de qualquer efeito financeiro.

### 9. Processamento síncrono por padrão

Não existe aceite assíncrono. `BET`, `WIN`, `LOSS`, `REFUND` e `ROLLBACK` concluem dentro do request HTTP ou do consumo SQS, sem commit intermediário de "aceitei, processo depois". O único caminho assíncrono é `PENDING_REFERENCE`, retomado pelo worker de referências.

**Por quê:** o enunciado permite concluir de forma síncrona operações sem dependências. Um processador genérico de `PENDING` dobraria a complexidade (claim, lease, testes de kill) sem ganho na rubrica. A retomada durável exigida fica coberta pelo worker de referências, pelo consumidor SQS e pelo publisher da outbox.

### 10. `202` só em `PENDING_REFERENCE`

Estados terminais respondem `200`/`422`/`409`; abertura de carteira responde `201`. `202 Accepted` é reservado a operações que ficaram aguardando referência.

**Por quê:** com a decisão 9, o caminho feliz é sempre terminal no mesmo ciclo. O `202` passa a significar exatamente uma coisa, o que torna o contrato testável.

### 11. Resultado do processamento persistido na transação

`result_balance_minor` + `result_balance_currency` e `result_payload` (snapshot JSONB da resposta) gravados no mesmo commit.

**Por quê:** o replay deve devolver o saldo observado no processamento original, mesmo que a carteira já tenha se movimentado depois. Recalcular pelo ledger no replay quebra com `LOSS`, rejeições e ordenação.

### 12. Hash estrito, sem normalização silenciosa

Só a forma canônica é aceita (`"25.00"`, duas casas, sem espaços, sem notação científica, moeda em maiúsculas). `"25.0"` é rejeitado com `INVALID_AMOUNT`. Hash SHA-256 sobre JSON canônico dos campos de negócio, sem a chave de idempotência nem metadados de transporte.

**Por quê:** o enunciado proíbe arredondamento silencioso e exige equivalência de hash entre HTTP e SQS. Rejeitar em vez de normalizar elimina uma fonte inteira de divergência.

### 13. Falhas: `REJECTED` para negócio, `FAILED` para infra permanente

Transitório entra em retry/backoff sem mudar estado; permanente ou esgotado vira `FAILED` auditável e a mensagem vai para a DLQ.

**Por quê:** misturar recusa de negócio com defeito de infraestrutura polui contrato, métricas e diagnóstico.

### 14. Testes como critério de Done

Ver a seção [Regra de testes](#regra-de-testes) abaixo.

## Regra de testes

> Nenhuma regra de negócio ou garantia do desafio entra como concluída sem teste automatizado que a prove; domínio e casos de uso com cobertura completa de branches; integração com infraestrutura real para persistência, mensageria, auth, concorrência e recuperação; `go test -race` obrigatório.

| Camada | Meta |
| --- | --- |
| Domínio (`Money`, `Wallet`, `WagerTransaction`, ledger) | **100% de cobertura, gate rígido** — abaixo disso a etapa não fecha |
| Casos de uso | 100% dos fluxos de decisão: replay, conflito de chave, conflito de `externalTransactionId`, rejeição, pendência, opening com e sem saldo |
| Adapters (HTTP, SQS, `pgx`) | 100% dos contratos e caminhos de falha, via integração com Postgres, LocalStack e Keycloak reais |
| `main`, wiring Fx, Dockerfile, clients AWS | Sem meta de percentual; validados por teste de composição (start/stop) e integração |

Proibido: asserção apenas de "não retornou erro" sem verificar saldo, ledger, status e `failureCode`; mock de Postgres, SQS ou IdP como prova de idempotência, lock ou recuperação (eliminatório no desafio).

## Regra de validação manual

> Toda rota nova entra na coleção Bruno (`bruno/`) junto com a implementação da etapa, e `make bruno` precisa passar antes de fechar a etapa.

A coleção é versionada no repositório e aberta com **Open Collection**, então evolui junto com o código sem exportar nem reimportar. Cada requisição documenta a regra de negócio que exercita e traz asserções quando o resultado é previsível. Guia completo em [`../bruno/README.md`](../bruno/README.md); decisão em ADR-022.

## Regra de entrega

> Cada etapa nasce em uma branch `etapa/NN-slug`, com PR aberto para `main`, e só é mergeada com o CI verde.

O CI roda em três frentes: build com análise estática, testes com `-race` mais o gate de cobertura do domínio, e integração subindo o Docker Compose para executar a coleção Bruno contra o ambiente real. O histórico fica com uma entrega por etapa, e o job de integração prova a cada PR que o ambiente sobe a partir de um checkout limpo — que é o critério de entrega do desafio. Decisão em ADR-023.

## Ordem de valor (o que mais pesa)

1. Integridade no banco (constraints + UoW)
2. Concorrência multi-instância demonstrável
3. Idempotência persistente + replay correto
4. Inbox/outbox + recuperação
5. Authn/z real com Keycloak
6. Testes com containers reais e `-race`
7. Observabilidade e documentação

## Fora do escopo (consciente)

- Partidas dobradas (opcional)
- OpenTelemetry / dashboards (diferencial)
- Testes de carga (diferencial, só se houver tempo)
- Aceite assíncrono genérico de operações (decisão 9)
- Assinatura criptográfica das mensagens SQS (controle fica em credencial do broker + validação de domínio)
- Multi-moeda operacional além de BRL nos cenários principais (tipo carrega moeda; testes de incompatibilidade obrigatórios)
