-- Schema inicial do serviço de apostas.
--
-- As invariantes financeiras são impostas aqui, no PostgreSQL, e não apenas na
-- aplicação: locks locais e a deduplicação do SQS FIFO não protegem contra um
-- processo com bug, uma instância antiga em execução ou uma escrita manual.

-- Carteiras -----------------------------------------------------------------

CREATE TABLE wallets (
    id            UUID        PRIMARY KEY,
    player_id     UUID        NOT NULL,
    currency      CHAR(3)     NOT NULL,
    balance_minor BIGINT      NOT NULL,
    version       BIGINT      NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,

    CONSTRAINT wallets_currency_is_iso4217 CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT wallets_balance_non_negative CHECK (balance_minor >= 0),
    CONSTRAINT wallets_version_positive CHECK (version >= 1),

    -- Um jogador tem no máximo uma carteira por moeda.
    CONSTRAINT wallets_player_currency_unique UNIQUE (player_id, currency),

    -- Alvo das chaves estrangeiras compostas abaixo: garantem no banco que toda
    -- movimentação e todo lançamento usam a moeda da própria carteira.
    CONSTRAINT wallets_id_currency_unique UNIQUE (id, currency)
);

-- Transações ----------------------------------------------------------------

CREATE TABLE wager_transactions (
    id       UUID PRIMARY KEY,
    origin   TEXT NOT NULL,
    kind     TEXT NOT NULL,
    status   TEXT NOT NULL,

    wallet_id       UUID    NOT NULL,
    player_id       UUID    NOT NULL,
    amount_minor    BIGINT  NOT NULL,
    amount_currency CHAR(3) NOT NULL,

    -- Metadados exclusivos da origem externa.
    provider_id                       TEXT,
    external_transaction_id           TEXT,
    idempotency_key                   TEXT,
    payload_hash                      TEXT,
    round_id                          TEXT,
    game_id                           TEXT,
    reference_external_transaction_id TEXT,
    reference_transaction_id          UUID REFERENCES wager_transactions (id),

    failure_code TEXT,

    -- Resultado devolvido ao provedor, congelado no processamento para que o
    -- replay reproduza o saldo da época em vez do saldo atual da carteira.
    result_balance_minor    BIGINT,
    result_balance_currency CHAR(3),
    result_wallet_version   BIGINT,
    result_payload          JSONB,

    -- Retentativa do worker de referências pendentes.
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT wager_transactions_wallet_currency_fk
        FOREIGN KEY (wallet_id, amount_currency) REFERENCES wallets (id, currency),

    CONSTRAINT wager_transactions_origin_valid
        CHECK (origin IN ('INTERNAL', 'EXTERNAL')),
    CONSTRAINT wager_transactions_kind_valid
        CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    CONSTRAINT wager_transactions_status_valid
        CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),

    CONSTRAINT wager_transactions_amount_non_negative CHECK (amount_minor >= 0),
    CONSTRAINT wager_transactions_currency_is_iso4217 CHECK (amount_currency ~ '^[A-Z]{3}$'),

    -- Política de valor por tipo: LOSS é o único que exige exatamente zero.
    CONSTRAINT wager_transactions_loss_amount_is_zero
        CHECK (kind <> 'LOSS' OR amount_minor = 0),
    CONSTRAINT wager_transactions_other_kinds_are_positive
        CHECK (kind = 'LOSS' OR amount_minor > 0),

    CONSTRAINT wager_transactions_reversal_requires_reference
        CHECK (kind NOT IN ('REFUND', 'ROLLBACK') OR reference_external_transaction_id IS NOT NULL),

    -- Origem interna e externa não se misturam: a abertura não carrega
    -- metadados de provedor, e a operação externa não pode ser um OPENING.
    CONSTRAINT wager_transactions_origin_metadata CHECK (
        (
            origin = 'INTERNAL'
            AND kind = 'OPENING'
            AND provider_id IS NULL
            AND external_transaction_id IS NULL
            AND idempotency_key IS NULL
            AND payload_hash IS NULL
            AND round_id IS NULL
            AND game_id IS NULL
            AND reference_external_transaction_id IS NULL
            AND reference_transaction_id IS NULL
        )
        OR (
            origin = 'EXTERNAL'
            AND kind <> 'OPENING'
            AND provider_id IS NOT NULL
            AND external_transaction_id IS NOT NULL
            AND idempotency_key IS NOT NULL
            AND payload_hash IS NOT NULL
            AND round_id IS NOT NULL
            AND game_id IS NOT NULL
        )
    ),

    -- Rejeição e falha exigem código estável; os demais estados não o têm.
    CONSTRAINT wager_transactions_failure_code_presence
        CHECK ((status IN ('REJECTED', 'FAILED')) = (failure_code IS NOT NULL)),

    -- Só uma operação concluída carrega resultado financeiro, e ele vem
    -- completo: valor, moeda e versão da carteira.
    CONSTRAINT wager_transactions_result_presence
        CHECK ((status = 'PROCESSED') = (result_balance_minor IS NOT NULL)),
    CONSTRAINT wager_transactions_result_consistency CHECK (
        (result_balance_minor IS NULL) = (result_balance_currency IS NULL)
        AND (result_balance_minor IS NULL) = (result_wallet_version IS NULL)
    ),
    CONSTRAINT wager_transactions_result_non_negative
        CHECK (result_balance_minor IS NULL OR result_balance_minor >= 0),

    CONSTRAINT wager_transactions_attempts_non_negative CHECK (attempt_count >= 0)
);

-- Idempotência: a mesma operação financeira nunca é aplicada duas vezes, mesmo
-- depois do reinício de todos os processos. O escopo é por provedor, porque a
-- chave pertence ao cliente e não deve cruzar tenants.
CREATE UNIQUE INDEX wager_transactions_provider_external_id_uq
    ON wager_transactions (provider_id, external_transaction_id)
    WHERE origin = 'EXTERNAL';

CREATE UNIQUE INDEX wager_transactions_provider_idempotency_key_uq
    ON wager_transactions (provider_id, idempotency_key)
    WHERE origin = 'EXTERNAL';

-- Crédito de abertura acontece uma única vez por carteira.
CREATE UNIQUE INDEX wager_transactions_single_opening_per_wallet_uq
    ON wager_transactions (wallet_id)
    WHERE kind = 'OPENING';

-- Uma referência não recebe duas reversões bem-sucedidas do mesmo tipo, o que
-- impede devolver o mesmo débito duas vezes.
CREATE UNIQUE INDEX wager_transactions_single_successful_reversal_uq
    ON wager_transactions (reference_transaction_id, kind)
    WHERE kind IN ('REFUND', 'ROLLBACK') AND status = 'PROCESSED';

CREATE INDEX wager_transactions_wallet_idx
    ON wager_transactions (wallet_id, created_at, id);

-- Resolução de referência por (provedor, id externo referenciado).
CREATE INDEX wager_transactions_reference_lookup_idx
    ON wager_transactions (provider_id, reference_external_transaction_id)
    WHERE reference_external_transaction_id IS NOT NULL;

-- Fila de trabalho do worker de referências pendentes.
CREATE INDEX wager_transactions_pending_reference_idx
    ON wager_transactions (next_retry_at)
    WHERE status = 'PENDING_REFERENCE';

-- Ledger --------------------------------------------------------------------

CREATE TABLE wallet_ledger_entries (
    id                   UUID        PRIMARY KEY,
    wallet_id            UUID        NOT NULL,
    transaction_id       UUID        NOT NULL REFERENCES wager_transactions (id),
    direction            TEXT        NOT NULL,
    amount_minor         BIGINT      NOT NULL,
    currency             CHAR(3)     NOT NULL,
    balance_before_minor BIGINT      NOT NULL,
    balance_after_minor  BIGINT      NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL,

    CONSTRAINT wallet_ledger_entries_wallet_currency_fk
        FOREIGN KEY (wallet_id, currency) REFERENCES wallets (id, currency),

    CONSTRAINT wallet_ledger_entries_direction_valid
        CHECK (direction IN ('DEBIT', 'CREDIT')),
    CONSTRAINT wallet_ledger_entries_amount_positive CHECK (amount_minor > 0),
    CONSTRAINT wallet_ledger_entries_balances_non_negative
        CHECK (balance_before_minor >= 0 AND balance_after_minor >= 0),

    -- A mesma equação validada no domínio, imposta também no banco.
    CONSTRAINT wallet_ledger_entries_balance_equation CHECK (
        (direction = 'CREDIT' AND balance_after_minor = balance_before_minor + amount_minor)
        OR (direction = 'DEBIT' AND balance_after_minor = balance_before_minor - amount_minor)
    ),

    -- Uma transação produz no máximo um lançamento por carteira: é a barreira
    -- final contra movimentação duplicada.
    CONSTRAINT wallet_ledger_entries_wallet_transaction_uq UNIQUE (wallet_id, transaction_id)
);

-- Sustenta o cursor opaco (created_at, id) com ordenação estável.
CREATE INDEX wallet_ledger_entries_cursor_idx
    ON wallet_ledger_entries (wallet_id, created_at, id);

-- O ledger é append-only: correção financeira exige lançamento novo. O gatilho
-- recusa UPDATE e DELETE inclusive para o dono da tabela, o que uma simples
-- revogação de privilégio não garantiria.
CREATE FUNCTION wallet_ledger_entries_reject_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'wallet_ledger_entries e append-only: % nao e permitido', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER wallet_ledger_entries_immutable
    BEFORE UPDATE OR DELETE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION wallet_ledger_entries_reject_mutation();

-- Inbox ---------------------------------------------------------------------

CREATE TABLE inbox_messages (
    id            UUID        PRIMARY KEY,
    consumer_name TEXT        NOT NULL,
    message_id    TEXT        NOT NULL,
    payload_hash  TEXT        NOT NULL,
    received_at   TIMESTAMPTZ NOT NULL,
    completed_at  TIMESTAMPTZ,

    -- Deduplicação da entrega at-least-once, independente do SQS FIFO.
    CONSTRAINT inbox_messages_consumer_message_uq UNIQUE (consumer_name, message_id)
);

-- Outbox --------------------------------------------------------------------

CREATE TABLE outbox_events (
    id             UUID        PRIMARY KEY,
    aggregate_type TEXT        NOT NULL,
    aggregate_id   UUID        NOT NULL,
    event_type     TEXT        NOT NULL,
    event_version  INTEGER     NOT NULL,
    payload        JSONB       NOT NULL,
    correlation_id TEXT        NOT NULL,
    causation_id   TEXT,
    occurred_at    TIMESTAMPTZ NOT NULL,

    attempts        INTEGER     NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    locked_by       TEXT,
    locked_until    TIMESTAMPTZ,
    published_at    TIMESTAMPTZ,

    CONSTRAINT outbox_events_event_version_positive CHECK (event_version >= 1),
    CONSTRAINT outbox_events_attempts_non_negative CHECK (attempts >= 0),

    -- O lease é gravado por inteiro ou não é gravado: um registro com dono e
    -- sem prazo ficaria travado para sempre.
    CONSTRAINT outbox_events_lease_consistency
        CHECK ((locked_by IS NULL) = (locked_until IS NULL))
);

-- Seleção de pendentes elegíveis pelo publisher, com SKIP LOCKED.
CREATE INDEX outbox_events_pending_idx
    ON outbox_events (next_attempt_at, id)
    WHERE published_at IS NULL;
