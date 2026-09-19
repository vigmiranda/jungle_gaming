# Etapa 6 — HTTP e autenticação/autorização

## Objetivo

Expor os contratos do item 9 do desafio com **IdP externo real** (Keycloak) e isolamento por `providerId`.

**Roteador:** `chi` (ADR-015), com middlewares de correlação (ADR-017), autenticação e logging.

## Done when

- [x] Rotas HTTP implementadas em `chi`
- [x] Middleware de `correlationId` (usa header recebido ou gera UUID) propagando ao caso de uso
- [x] Validação JWT via JWKS do Keycloak
- [x] Provedores só acessam as próprias transações (inclui replay/consulta)
- [x] Operações de carteira internas restritas ao serviço interno
- [x] Códigos HTTP distinguíveis por situação
- [x] `GET /health/live` e `GET /health/ready` (Postgres + SQS)
- [x] Testes de integração: token ausente/inválido/expirado; isolamento entre provedores
- [x] Todas as rotas desta etapa presentes na coleção Bruno, com `make bruno` verde (ADR-022)

## Endpoints

| Método | Rota | Quem |
| --- | --- | --- |
| `POST` | `/wallets` | serviço interno |
| `GET` | `/wallets/:walletId` | serviço interno |
| `GET` | `/wallets/:walletId/ledger?cursor=&limit=` | serviço interno |
| `POST` | `/wallets/:walletId/reconciliation` | serviço interno |
| `POST` | `/wagering/transactions` | provedor (próprio `providerId`) |
| `GET` | `/wagering/transactions/:transactionId` | interno, ou provedor só das próprias |
| `GET` | `/providers/:providerId/wagering/transactions/:externalTransactionId` | interno, ou provedor = path |
| `GET` | `/health/live` | público |
| `GET` | `/health/ready` | público |

Header obrigatório em POST de operação: `Idempotency-Key` (não substituir silenciosamente).

## Autorização

| Identidade | Pode |
| --- | --- |
| Client do provedor | apenas `providerId` do token; POST/GET do próprio provedor |
| Serviço interno | abertura de carteira, reconciliação, leituras internas |
| Qualquer anônimo | só health |

Acesso não autorizado **não** pode gerar efeito financeiro nem vazar dados.

## Mapeamento HTTP sugerido

| Situação | Status |
| --- | --- |
| Entrada inválida | 400 |
| Sem/ inválido / expirado | 401 |
| Provedor errado / operação interna | 403 |
| Conflito (carteira, chave vs payload, externalId) | 409 |
| Rejeição de negócio | 422 (+ `failureCode`) |
| Processado | 200 |
| Abertura de carteira criada | 201 |
| `PENDING_REFERENCE` | 202 |
| Infra indisponível | 503 |

Mapeamento fechado em ADR-013. Como o processamento é síncrono (ADR-012), `202` na prática ocorre apenas em reversão que chegou antes da referência — não existe "aceito, processo depois" para as demais operações.

## Políticas de leitura (fechadas nesta etapa)

- `GET /wallets/*` e ledger: **somente serviço interno**
- `GET /wagering/transactions/:transactionId`: provedor vê apenas as próprias; interno vê todas
- `GET /providers/:providerId/...`: path `providerId` deve coincidir com o token do provedor

## Provisionamento Keycloak

- Realm + clients (`client_credentials`)
- Identidades de teste no Compose / scripts
- Instruções no README para obter token e chamar a API

## Por quê esta abordagem

- Autenticação efetiva nos endpoints de negócio é **eliminatória**.
- Keycloak no Compose é a recomendação do enunciado e torna o fluxo reproduzível no checkout limpo.
- Isolamento por provedor também vale para replays — ponto fácil de esquecer.

## Observações

- Mensageria: credenciais e políticas do broker; domínio continua validando o `providerId` no consumidor (ADR-008).
- Não implementar autenticação caseira (senha/token próprios).
