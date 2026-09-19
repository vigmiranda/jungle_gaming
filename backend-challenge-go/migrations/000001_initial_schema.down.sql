-- Reversão do schema inicial.
--
-- A ordem inverte as dependências: lançamentos e transações antes das
-- carteiras, que são referenciadas por chaves estrangeiras compostas.

DROP TRIGGER IF EXISTS wallet_ledger_entries_immutable ON wallet_ledger_entries;
DROP FUNCTION IF EXISTS wallet_ledger_entries_reject_mutation();

DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS inbox_messages;
DROP TABLE IF EXISTS wallet_ledger_entries;
DROP TABLE IF EXISTS wager_transactions;
DROP TABLE IF EXISTS wallets;
