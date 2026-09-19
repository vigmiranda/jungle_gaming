# Coleção Bruno — Wagering API

Coleção usada para validar a API manualmente a cada etapa do roadmap. Ela vive
no repositório, versionada junto com o código: as requisições novas chegam pelo
`git pull`, sem exportar nem reimportar nada.

## Índice

- [Pré-requisitos](#pré-requisitos)
- [Importar no aplicativo](#importar-no-aplicativo)
- [Executar pela linha de comando](#executar-pela-linha-de-comando)
- [Ambientes e variáveis](#ambientes-e-variáveis)
- [Fluxo de validação](#fluxo-de-validação)
- [O que já funciona](#o-que-já-funciona)
- [Como adicionar uma requisição](#como-adicionar-uma-requisição)
- [Problemas comuns](#problemas-comuns)

## Pré-requisitos

Suba o ambiente local antes de qualquer chamada:

```sh
docker compose up -d
docker compose ps
```

Os quatro serviços precisam estar `healthy` (a API não tem healthcheck próprio;
confirme por `GET /health/ready`). As portas padrão são API `8080`, Keycloak
`8088`, PostgreSQL `5432` e LocalStack `4566`.

Para a interface gráfica, instale o [Bruno](https://www.usebruno.com/downloads).
Para a linha de comando basta Node.js 18 ou superior.

## Importar no aplicativo

1. Abra o Bruno.
2. Menu **Collection → Open Collection**.
3. Selecione a pasta `bruno/` deste repositório e confirme.
4. No seletor de ambiente, no canto superior direito, escolha **Local**.

A coleção aparece na barra lateral com as pastas numeradas na ordem de uso.

Não use **Import**: essa opção converte a coleção para o armazenamento interno
do Bruno e o vínculo com o repositório se perde. **Open Collection** mantém os
arquivos `.bru` como fonte da verdade, que é o que faz a coleção evoluir junto
com o código.

## Executar pela linha de comando

Útil para validar tudo de uma vez, e é assim que a coleção é verificada a cada
entrega:

```sh
make bruno
```

Equivalente direto, sem o Make:

```sh
cd bruno
npx --yes @usebruno/cli@latest run . --env Local
```

Para rodar só uma parte, passe as pastas:

```sh
npx --yes @usebruno/cli@latest run "01 - Health" "02 - Auth" --env Local
```

A saída lista cada requisição com o status HTTP e o resultado das asserções,
terminando em um resumo com o total de falhas. O comando devolve código de saída
diferente de zero quando alguma asserção falha, então serve em pipeline.

## Ambientes e variáveis

O ambiente **Local** (`environments/Local.bru`) concentra endereços, credenciais
e os identificadores usados nas chamadas.

| Variável | Uso |
| --- | --- |
| `baseUrl`, `keycloakUrl`, `realm` | Endereços do ambiente local |
| `providerClientId` / `Secret` | Client do provedor A |
| `otherProviderClientId` / `Secret` | Client do provedor B, para testes de isolamento |
| `internalClientId` / `Secret` | Client do serviço interno |
| `playerId`, `roundId`, `gameId` | Dados de negócio das operações |
| `externalTransactionId` | Chave externa da aposta usada como referência |

Três variáveis são preenchidas em tempo de execução pelas próprias requisições,
via bloco `vars:post-response`:

| Variável | Preenchida por |
| --- | --- |
| `providerToken`, `otherProviderToken`, `internalToken` | Requisições de `02 - Auth` |
| `walletId` | `03 - Carteiras / Abrir carteira` |
| `transactionId` | `04 - Operacoes / BET` |

Por isso a ordem importa: execute a pasta de auth antes das demais, e a abertura
de carteira antes das operações.

## Fluxo de validação

| Ordem | Pasta | O que valida |
| --- | --- | --- |
| 1 | `01 - Health` | Processo no ar e dependências prontas |
| 2 | `02 - Auth` | Emissão de token no Keycloak por `client_credentials` |
| 3 | `03 - Carteiras` | Abertura, consulta, ledger e reconciliação |
| 4 | `04 - Operacoes` | Os cinco tipos externos, replay idempotente e conflitos |
| 5 | `05 - Consultas` | Acompanhamento por id interno e por chave do provedor |
| 6 | `06 - Autorizacao` | Credencial ausente, isolamento entre provedores e operação interna |

Cada requisição documenta no campo `docs` a regra de negócio que exercita, com a
etapa do roadmap em que passa a funcionar.

## O que já funciona

| Pasta | Situação |
| --- | --- |
| `01 - Health` | Funcionando (etapa 1) |
| `02 - Auth` | Funcionando (etapa 1) |
| `03 - Carteiras` | Funcionando (etapa 6) |
| `04 - Operacoes` | Funcionando (etapa 6) |
| `05 - Consultas` | Funcionando (etapa 6) |
| `06 - Autorizacao` | Funcionando (etapa 6) |

## Como adicionar uma requisição

**Toda rota nova da API entra na coleção junto com a implementação.** Uma etapa
não é considerada concluída se a requisição correspondente não estiver aqui.

1. Crie o arquivo `.bru` na pasta numerada correspondente. O nome do arquivo é o
   nome exibido; evite acentos para não depender da codificação do sistema.
2. Ajuste o `seq` para posicionar a requisição na ordem de execução da pasta.
3. Use as variáveis do ambiente em vez de valores fixos, principalmente para
   identificadores e credenciais.
4. Descreva no bloco `docs` a regra de negócio exercitada e a etapa do roadmap.
5. Adicione asserções nos casos com resultado previsível (status, `failureCode`,
   `idempotentReplay`).
6. Rode `make bruno` antes de concluir a etapa.

Modelo mínimo:

```
meta {
  name: Nome exibido
  type: http
  seq: 1
}

post {
  url: {{baseUrl}}/caminho
  body: json
  auth: bearer
}

auth:bearer {
  token: {{providerToken}}
}

headers {
  Content-Type: application/json
  X-Correlation-Id: bruno-{{$randomUUID}}
}

body:json {
  {
    "campo": "{{variavelDoAmbiente}}"
  }
}

vars:post-response {
  algumId: res.body.id
}

docs {
  O que esta requisição prova e em qual etapa passa a funcionar.
}

assert {
  res.status: eq 200
}
```

## Problemas comuns

**Todas as requisições falham com conexão recusada.** O ambiente não está no ar
ou a porta foi remapeada. Confira `docker compose ps` e, se tiver ajustado
alguma `*_HOST_PORT` no `.env`, atualize `baseUrl` e `keycloakUrl` no ambiente
**Local**.

**401 em rotas de negócio com o token recém-obtido.** O access token do Keycloak
expira em cinco minutos; execute novamente a requisição de token da pasta
`02 - Auth`.

**404 em carteiras ou operações.** Esperado até a etapa 5 do roadmap.

**Variável vazia no meio do fluxo.** A requisição que a preenche não rodou nesta
sessão. Execute a pasta inteira na ordem, ou use `make bruno`, que roda tudo em
sequência.

## Credenciais

Os secrets do ambiente **Local** são os mesmos do realm importado pelo Compose
em `deploy/keycloak/realm-wagering.json`. São valores de desenvolvimento, sem
uso fora do ambiente local.

| Client | Claims | Secret |
| --- | --- | --- |
| `internal-service` | `wallet_role: internal` | `internal-service-secret` |
| `provider-a` | `provider_id: provider-a`, `wallet_role: provider` | `provider-a-secret` |
| `provider-b` | `provider_id: provider-b`, `wallet_role: provider` | `provider-b-secret` |
