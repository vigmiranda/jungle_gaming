# Etapa 11 — Testes obrigatórios

## Objetivo

Provar as garantias com `testing`/`go test`, infraestrutura real e `-race` — sem substituir Postgres, SQS e IdP por mocks em bloco.

## Regra de testes (ADR-019)

> Nenhuma regra de negócio ou garantia do desafio entra como concluída sem teste automatizado que a prove.

| Camada | Meta |
| --- | --- |
| Domínio | 100% de cobertura, gate rígido |
| Casos de uso | 100% de cobertura, gate rígido |
| Adapters HTTP / SQS / `pgx` | 100% dos contratos e caminhos de falha, com infra real |
| `main`, wiring Fx, Dockerfile, clients AWS | Sem meta de percentual; teste de composição + integração |

Proibido: asserção apenas de "não retornou erro" sem verificar saldo, ledger, status e `failureCode`; mock de Postgres, SQS ou IdP como prova de idempotência, lock ou recuperação.

Esta etapa fecha a bateria oficial, mas os testes são escritos **junto com cada fatia**, desde a Wave 1.

## Done when

- [ ] Unitários de domínio completos, com relatório de cobertura anexado ao README
- [ ] Integração com containers reais (Compose/testcontainers)
- [ ] Auth real com Keycloak
- [ ] Os 8 cenários de concorrência/recuperação do README
- [ ] Cruzamento HTTP × SQS
- [ ] Reconciliação final saldo × ledger
- [ ] Verificação Fx start/stop e liberação de workers
- [ ] Comandos: `go test ./...`, `go test -race ./...`, `go vet ./...`, cobertura do domínio

## Unitários

- Money: parse, escala, limites, inválidos, moedas incompatíveis, formas não canônicas rejeitadas (`"25.0"`, espaços, moeda minúscula)
- Wallet: invariantes, débito/crédito, versão
- Estados e transições de `WagerTransaction`
- Regras dos cinco tipos externos + política de zero
- OPENING interno (metadados e eventos)
- Conflito de payload para a mesma chave (hash)

## Integração (containers reais)

- Migrations up/down
- Constraints e imutabilidade do ledger
- Atomicidade financeira
- Uniques de idempotência por provedor: chave reutilizada com payload diferente, `externalTransactionId` com outra chave
- Replay devolve o saldo da época mesmo após novas movimentações na carteira (`result_balance_*` / `result_payload`)
- Paginação do ledger por cursor opaco: ordenação estável com inserts concorrentes; cursor inválido → 400
- Inbox + reentrega
- Outbox concorrente com lease expirado reassumido
- Retry + DLQ, com `FAILED` distinto de `REJECTED`
- `providerId` não autorizado via SQS não gera efeito financeiro
- Recuperação após reinicialização
- Composição Fx (start/stop)

## Autenticação e autorização

- Credenciais ausentes, inválidas, expiradas → rejeitadas
- Isolamento entre provedores (consulta e replay)
- Restrição de operações internas
- Sem efeito financeiro em acesso não autorizado

## Concorrência e recuperação (checklist do desafio)

1. Mesma aposta **50×** em paralelo → um único débito
2. Duas apostas de **80.00** sobre **100.00** → 1 processed, 1 rejected, saldo **20.00**, um débito no ledger; reenvios não alteram
3. Carteiras distintas em paralelo
4. Cenários relevantes com **≥ 3 instâncias** independentes (três processos locais contra o mesmo Compose — ADR-020)
5. Interromper consumidor após commit e antes do delete SQS → reentrega segura
6. Dois publishers na mesma outbox → recuperação de publicação (matar um durante o lease)
7. `REFUND`/`ROLLBACK` antes da referência → resolução posterior ou rejeição por expiração
8. Restart com idempotência/pendências preservadas. Como não há aceite assíncrono (ADR-012), a retomada exercitada é a de `PENDING_REFERENCE`, do consumidor SQS e do publisher da outbox — documentar essa interpretação

Ao final: saldo armazenado = créditos − débitos do ledger. Incluir cenários que cruzam HTTP e SQS.

## Por quê esta abordagem

- 10 pontos de testes + vários eliminatórios (mocks totais, single-instance, race não exercitado).
- Três processos independentes são a evidência de que o lock/idempotência não vivem na memória local.
- `-race` pega compartilhamento indevido entre goroutines de workers.

## Organização sugerida

```
internal/domain/...        # unitários rápidos
tests/integration/...      # build tag integration (opcional)
Makefile / scripts        # sobe deps, roda suíte e as 3 instâncias
```

Documentar como preparar dependências e como rodar integração, multi-instância e simulação de falha.
