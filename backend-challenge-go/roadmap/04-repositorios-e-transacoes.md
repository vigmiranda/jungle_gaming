# Etapa 4 — Repositórios e transações

## Objetivo

Persistir o domínio com SQL explícito (`pgx`), delimitando **uma** unidade de trabalho financeira por operação.

## Done when

- [ ] Ports de repositório definidos no domínio/aplicação
- [ ] Implementação `pgx` com SQL explícito
- [ ] `UnitOfWork` / `Tx` compartilhada entre repositórios
- [ ] Lock por carteira (`SELECT ... FOR UPDATE`) demonstrável
- [ ] Documentação de onde começa e termina a transação SQL
- [ ] Teste: duas conexões disputam a mesma carteira sem lost update

## Unidade de trabalho (ordem típica no mesmo `BEGIN…COMMIT`)

1. Lock da carteira
2. Insert/update da `WagerTransaction` (ou lookup de replay)
3. Ledger (se houver movimentação)
4. Update de saldo + `version`
5. Resultado do processamento (`result_balance_*`, `result_payload`) — ADR-014
6. Inbox (se entrada SQS)
7. Outbox (eventos correspondentes)

Tudo que for efeito financeiro observável deve estar neste commit.

## Estratégia de concorrência

**Decisão (ADR-004):** locking pessimista **puro** por linha da carteira. Não há retry otimista por conflito de versão.

- Duas instâncias, mesma carteira: uma espera; a segunda lê saldo atualizado e aplica a regra (ex.: rejeita BET sem saldo).
- Carteiras distintas: sem contenção.
- `version` é escrita pelo agregado (invariante + payload de `WalletBalanceChanged`), não usada como predicado de `UPDATE`.

**Proibido:** lock global (mutex de processo, `LOCK TABLE` da carteira inteira do sistema, fila única global).

## Replay e leitura

- Consultas de GET podem ser read-only fora da UoW de escrita.
- Replay de operação concluída **não** reabre débito/crédito; devolve `result_balance_*` / `result_payload` persistidos, sem ler o saldo atual da carteira e sem recalcular pelo ledger.
- Paginação do ledger por cursor opaco `(created_at, id)`, nunca por offset (ADR-018).

## Por quê esta abordagem

- O README pede delimitar a transação SQL entre repositórios e documentar a biblioteca.
- Sem UoW, HTTP e SQS divergem e a atomicidade inbox/domínio/outbox some.
- `FOR UPDATE` por carteira atende o teste 80+80 vs 100 e o requisito de paralelismo entre carteiras.

## Checklist de recuperação (preparação)

| Falha | Comportamento esperado |
| --- | --- |
| Crash antes do commit | Nada persistido; reentrega segura |
| Crash depois do commit, antes do delete SQS | Inbox/domínio ok; reentrega é no-op |
| Crash depois do commit, antes do publish | Outbox pendente; outro publisher assume |
