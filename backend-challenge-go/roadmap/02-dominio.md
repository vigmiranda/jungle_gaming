# Etapa 2 — Domínio puro

## Objetivo

Modelar entidades com estado encapsulado, construtores com validação e transições explícitas — **sem** HTTP, SQS, Fx ou SQL.

## Done when

- [x] `Money` imutável com parse/serialização decimal string
- [x] `Wallet` com débito/crédito, versão, moeda e saldo ≥ 0
- [x] `WagerTransaction` com máquina de estados
- [x] `WalletLedgerEntry` com validação `balanceAfter = balanceBefore ± money`
- [x] Separação criação vs reidratação
- [x] Erros classificáveis (`errors.Is` / `errors.As`); sem `panic` de negócio
- [x] Testes unitários cobrindo Money, carteira, estados, 5 tipos externos, zero policy e OPENING
- [x] 100% de cobertura nos pacotes de domínio (gate rígido — ADR-019)
- [x] `go test -race` nos pacotes de domínio

## Notas de implementação

| Pacote | Conteúdo |
| --- | --- |
| `internal/domain/shared` | `Error` classificável por código, `ID` em UUIDv7 |
| `internal/domain/money` | `Money` em `int64` de centavos, `Currency` ISO 4217, parsing estrito |
| `internal/domain/wallet` | Agregado com `Open`, `Rehydrate`, `Credit`, `Debit` e `Movement` |
| `internal/domain/ledger` | `Entry` imutável com validação da equação de saldo |
| `internal/domain/wagering` | `Kind`, `Status`, `FailureCode` e a transação com máquina de estados |

- `Movement` devolve valor, saldo anterior, saldo posterior e versão: a borda não recalcula o que o agregado já sabe, e esses são exatamente os campos de `WalletBalanceChanged`.
- `Result` (saldo + versão da carteira) fica na transação em `MarkProcessed`, sustentando o replay com o saldo da época (ADR-014).
- Dois testes internos (`wallet`, `shared`) cobrem defesas inalcançáveis pela API pública — estouro na subtração a partir de saldo corrompido e falha da fonte de entropia — sem afrouxar validações para fins de cobertura.

## Entidades

### Money

- Value object: valor + moeda
- Entrada externa: `{"amount":"25.00","currency":"BRL"}`
- Escala fixa 2 casas; rejeitar vazio, `NaN`, `Infinity`, científico, escala excedente, negativo na borda externa
- **Parsing estrito (ADR-021):** só a forma canônica é aceita; `"25.0"`, `" 25.00"` e `"brl"` são rejeitados, não normalizados
- Sem arredondamento silencioso
- Aritmética exige moedas compatíveis
- Overflow tratado em parse/soma/sub/negação
- Negativos permitidos só em diferenças internas; saldo da carteira nunca negativo
- Representação recomendada: `int64` em centavos; documentar limites

### Wallet

- Agregado: id, playerId, currency, balance, version, createdAt, updatedAt
- Identidade: `(playerId, currency)` única
- Versão inicial `1`; incrementa só com mudança de saldo — é invariante de domínio e campo de evento, **não** mecanismo de concorrência (ADR-004)
- Débito/crédito sob controle do agregado
- Reidratação **não** reaplica movimentações nem emite eventos

### WagerTransaction

Tipos: `OPENING`, `BET`, `WIN`, `LOSS`, `REFUND`, `ROLLBACK`.

Estados:

| Estado | Significado |
| --- | --- |
| `PENDING` | Aceito, não concluído |
| `PENDING_REFERENCE` | Espera referência |
| `PROCESSED` | Sucesso terminal |
| `REJECTED` | Regra de negócio terminal |
| `FAILED` | Falha permanente de infra (auditoria) |

- Terminal não transita de novo
- Replay consulta resultado persistido sem reaplicar
- `OPENING` só origem interna; rejeitar se vier por HTTP/SQS
- `PENDING` é transitório dentro da própria transação: não há aceite assíncrono (ADR-012)
- `FAILED` é exclusivo de falha permanente de infraestrutura; recusa de negócio é `REJECTED` (ADR-016)

### WalletLedgerEntry

- `DEBIT` / `CREDIT`, balanceBefore/After, transactionId, walletId
- Imutável após construção
- `LOSS` e rejeições **não** produzem lançamento

## Regras por tipo (resumo)

| Tipo | Movimentação | Condição |
| --- | --- | --- |
| `BET` | Débito | Valor > 0, saldo suficiente |
| `WIN` | Crédito | Valor > 0 |
| `LOSS` | Nenhuma | amount `"0.00"`; sem ledger; sem bump de versão |
| `REFUND` | Crédito | Devolve BET processada integralmente |
| `ROLLBACK` | Inverso | Desfaz BET/WIN/REFUND processada |

## Por quê esta abordagem

- 20 pontos de integridade financeira começam aqui: invariantes testáveis sem banco.
- Separar criação e reidratação evita “double apply” ao carregar do Postgres.
- Erros tipados permitem mapear `failureCode` estável no contrato HTTP depois.

## Testes mínimos desta etapa

1. Parse Money válido/inválido e overflow
2. Incompatibilidade de moedas
3. Débito insuficiente
4. Transições ilegais de estado
5. LOSS não altera saldo/versão
6. OPENING interno vs rejeição externa
7. Ledger: equação balanceBefore/After
