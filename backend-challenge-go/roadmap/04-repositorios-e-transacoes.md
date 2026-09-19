# Etapa 4 — Repositórios e transações

## Objetivo

Persistir o domínio com SQL explícito (`pgx`), delimitando **uma** unidade de trabalho financeira por operação.

## Done when

- [x] Ports de repositório definidos na camada de aplicação
- [x] Implementação `pgx` com SQL explícito
- [x] `UnitOfWork` compartilhada entre repositórios
- [x] Lock por carteira (`SELECT ... FOR UPDATE`) demonstrável
- [x] Documentação de onde começa e termina a transação SQL
- [x] Teste: duas conexões disputam a mesma carteira sem lost update

## Notas de implementação

| Item | Onde |
| --- | --- |
| Ports | `internal/application/port` — interfaces do lado de quem consome |
| Implementação | `internal/platform/postgres/repository` |
| Transação | `UnitOfWork.Execute` abre, entrega os repositórios e confirma; `ReadOnly()` serve as leituras sem lock |
| Erros | SQLSTATE traduzido para `port.ErrNotFound` e `port.ErrConflict`, com o nome da constraint na mensagem |

- Nenhum repositório abre commit: a transação começa e termina em `Execute`, e todos os repositórios daquela chamada compartilham a mesma `pgx.Tx`.
- `UpdateBalance` não usa predicado de versão. A linha já está bloqueada pelo `FOR UPDATE`, que é o mecanismo de concorrência (ADR-004); a versão é invariante de domínio e payload de evento.
- Toda escrita confirma que atingiu exatamente uma linha: um `UPDATE` que não encontra a linha é falha de premissa, não sucesso silencioso.
- `ListByWallet` busca uma linha além do limite para descobrir se existe página seguinte sem uma contagem extra.

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
