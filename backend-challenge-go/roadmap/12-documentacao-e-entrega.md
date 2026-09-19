# Etapa 12 — Documentação e entrega

## Objetivo

Permitir que outra pessoa reproduza a solução a partir de um checkout limpo e entenda as decisões técnicas.

## Done when

- [ ] `README.md` da solução completo
- [ ] `ARCHITECTURE.md` com todas as decisões pedidas
- [ ] `.env.example`
- [ ] Compose + provisionamento automático do IdP e identidades de teste
- [ ] Migrations com aplicação e reversão documentadas
- [ ] Comandos oficiais funcionando
- [ ] Coleção Bruno completa e documentada, cobrindo todas as rotas entregues (ADR-022)
- [ ] Código `gofmt`; módulos reproduzíveis (`go.mod`/`go.sum`)

## README da solução (conteúdo mínimo)

1. Pré-requisitos
2. Variáveis de ambiente
3. Inicialização das filas (entrada FIFO + DLQ + fila de integração da outbox)
4. Aplicação e reversão das migrations
5. Subir a aplicação
6. Subir três instâncias locais para os cenários multi-instância (ADR-020)
7. Exemplos de chamadas autenticadas
8. Comandos de teste (unitário, integração, race, cobertura, multi-instância, falhas)
9. Limitações conhecidas

## ARCHITECTURE.md (conteúdo mínimo)

Decisões sobre:

- Dinheiro (`Money`), limites e parsing estrito sem normalização (ADR-002, ADR-021)
- Transações SQL / delimitação entre repositórios
- Idempotência, escopo por provedor e hash canônico (ADR-005, ADR-021)
- Locks / concorrência, com o papel da coluna `version` (ADR-004)
- Modelo de processamento síncrono e ausência de aceite assíncrono (ADR-012)
- Resultado persistido usado no replay (ADR-014)
- Referências pendentes e política por estado da referência (ADR-009)
- Política de REFUND × ROLLBACK
- Inbox / outbox, lease do publisher e destino dos eventos (ADR-006)
- Classificação `REJECTED` × `FAILED` × DLQ (ADR-016)
- Correlação e identidade de eventos (ADR-017)
- Autenticação e autorização (IdP, claims, papéis) e validação de `providerId` no SQS (ADR-007, ADR-008)
- Uso do Uber Fx e shutdown
- Estratégia e cobertura de testes (ADR-019)
- Interpretações adotadas
- Trabalho não concluído / diferenciais não feitos

Usar [decisoes-arquiteturais.md](./decisoes-arquiteturais.md) como rascunho vivo durante o desenvolvimento.

## Comandos mínimos

```sh
docker compose up --build
go test ./...
go test -race ./...
go vet ./...
go test -coverprofile=coverage.out ./internal/domain/... && go tool cover -func=coverage.out
```

Documentar equivalentes se houver build tags ou scripts.

## Por quê esta abordagem

- 5 pontos de documentação e critério de entrega.
- Em entrevista de líder técnico, a clareza das decisões pesa tanto quanto o código.
- Rascunhar ARCHITECTURE desde a etapa 0 evita documentação “de memória” no último dia.

## Checklist final anti-eliminatória

- [ ] Auth efetiva em endpoints de negócio
- [ ] Sem float em dinheiro
- [ ] Sem saldo negativo por concorrência
- [ ] Sem movimentação duplicada
- [ ] Idempotência persistente
- [ ] Funciona com múltiplas instâncias
- [ ] Sem publish antes do commit
- [ ] Ledger auditável append-only
- [ ] Testes com Postgres + SQS + IdP reais
- [ ] Todo caso do enunciado tem teste correspondente (ADR-019)
