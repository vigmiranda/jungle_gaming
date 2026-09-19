package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// As transições de domínio chamadas pelo caso de uso não podem falhar quando o
// fluxo chega até elas: o estado já foi validado. As proteções abaixo existem
// para o caso de uma refatoração quebrar essa premissa, e são exercitadas aqui
// chamando os métodos diretamente com estado inconsistente — sem afrouxar as
// validações do caminho público.

// noopRepositories aceita qualquer escrita: o objeto sob teste é a transição de
// domínio, não a persistência.
type noopRepositories struct{}

func (noopRepositories) Wallets() port.WalletRepository           { return noopWallets{} }
func (noopRepositories) Transactions() port.TransactionRepository { return noopTransactions{} }
func (noopRepositories) Ledger() port.LedgerRepository            { return noopLedger{} }
func (noopRepositories) Inbox() port.InboxRepository              { return noopInbox{} }
func (noopRepositories) Outbox() port.OutboxRepository            { return noopOutbox{} }

type noopInbox struct{}

func (noopInbox) Record(context.Context, port.InboxMessage) error { return nil }
func (noopInbox) Find(context.Context, string, string) (port.InboxMessage, error) {
	return port.InboxMessage{}, port.ErrNotFound
}

type noopOutbox struct{}

func (noopOutbox) Append(context.Context, port.OutboxRecord) error { return nil }
func (noopOutbox) Claim(context.Context, string, int, time.Duration, time.Time) ([]port.OutboxRecord, error) {
	return nil, nil
}
func (noopOutbox) MarkPublished(context.Context, shared.ID, time.Time) error { return nil }
func (noopOutbox) ReleaseWithBackoff(context.Context, shared.ID, int, time.Time, time.Time) error {
	return nil
}

type noopWallets struct{}

func (noopWallets) Create(context.Context, *wallet.Wallet) error { return nil }
func (noopWallets) LockByID(context.Context, shared.ID) (*wallet.Wallet, error) {
	return nil, port.ErrNotFound
}
func (noopWallets) FindByID(context.Context, shared.ID) (*wallet.Wallet, error) {
	return nil, port.ErrNotFound
}
func (noopWallets) UpdateBalance(context.Context, *wallet.Wallet) error { return nil }

type noopTransactions struct{}

func (noopTransactions) Create(context.Context, *wagering.Transaction) error { return nil }
func (noopTransactions) Update(context.Context, *wagering.Transaction) error { return nil }
func (noopTransactions) FindByID(context.Context, shared.ID) (*wagering.Transaction, error) {
	return nil, port.ErrNotFound
}
func (noopTransactions) FindByIdempotencyKey(context.Context, string, string) (*wagering.Transaction, error) {
	return nil, port.ErrNotFound
}
func (noopTransactions) FindByExternalID(context.Context, string, string) (*wagering.Transaction, error) {
	return nil, port.ErrNotFound
}
func (noopTransactions) HasSuccessfulReversal(context.Context, shared.ID) (bool, error) {
	return false, nil
}

type noopLedger struct{}

func (noopLedger) Append(context.Context, ledger.Entry) error { return nil }
func (noopLedger) ListByWallet(context.Context, shared.ID, *port.LedgerCursor, int) (port.LedgerPage, error) {
	return port.LedgerPage{}, nil
}
func (noopLedger) SumByWallet(context.Context, shared.ID) (money.Money, int, error) {
	return money.Money{}, 0, nil
}

var internalNow = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func internalID(t *testing.T, raw string) shared.ID {
	t.Helper()
	parsed, err := shared.ParseID(raw)
	if err != nil {
		t.Fatalf("ParseID devolveu erro: %v", err)
	}
	return parsed
}

func internalMoney(t *testing.T, amount string) money.Money {
	t.Helper()
	parsed, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("Parse devolveu erro: %v", err)
	}
	return parsed
}

func internalWallet(t *testing.T) *wallet.Wallet {
	t.Helper()
	opened, err := wallet.Open(
		internalID(t, "0192f291-27dd-7d3f-8071-5f8685deef37"),
		internalID(t, "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"),
		internalMoney(t, "1000.00"), internalNow)
	if err != nil {
		t.Fatalf("Open devolveu erro: %v", err)
	}
	return opened
}

func internalTransaction(t *testing.T, kind wagering.Kind, reference string) *wagering.Transaction {
	t.Helper()
	created, err := wagering.NewExternal(wagering.ExternalParams{
		ID:                  internalID(t, "0192f298-345e-7e38-af88-e43f851a819d"),
		Kind:                kind,
		WalletID:            internalID(t, "0192f291-27dd-7d3f-8071-5f8685deef37"),
		PlayerID:            internalID(t, "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"),
		Amount:              internalMoney(t, "25.00"),
		ProviderID:          "provider-a",
		ExternalID:          "transaction-123",
		IdempotencyKey:      "provider-a:transaction-123",
		PayloadHash:         "hash",
		RoundID:             "round-987",
		GameID:              "fortune-chimp",
		ReferenceExternalID: reference,
		CreatedAt:           internalNow,
	})
	if err != nil {
		t.Fatalf("NewExternal devolveu erro: %v", err)
	}
	return created
}

func internalUseCase() *ProcessWagerTransaction {
	return NewProcessWagerTransaction(nil, nil, fixedIDs{})
}

type fixedIDs struct{}

func (fixedIDs) NewID() (shared.ID, error) {
	return shared.ParseID("0192f2a0-0000-7000-8000-00000000000a")
}

func TestSettleRefusesTerminalTransaction(t *testing.T) {
	transaction := internalTransaction(t, wagering.Bet, "")
	if err := transaction.Reject(wagering.FailureInsufficientFunds, internalNow); err != nil {
		t.Fatalf("Reject devolveu erro: %v", err)
	}

	_, err := internalUseCase().settle(
		context.Background(), noopRepositories{}, internalWallet(t), transaction, nil, internalNow)

	if err == nil {
		t.Error("esperava recusa ao concluir uma transação já terminal")
	}
}

func TestSettleRefusesInconsistentMovement(t *testing.T) {
	target := internalWallet(t)
	inconsistent := &settlement{
		direction: ledger.Debit,
		movement: wallet.Movement{
			Amount:        internalMoney(t, "25.00"),
			BalanceBefore: internalMoney(t, "1000.00"),
			BalanceAfter:  internalMoney(t, "999.00"),
			Version:       2,
		},
	}

	_, err := internalUseCase().settle(
		context.Background(), noopRepositories{}, target,
		internalTransaction(t, wagering.Bet, ""), inconsistent, internalNow)

	if err == nil {
		t.Error("esperava recusa de um lançamento que não fecha a equação de saldo")
	}
}

func TestRejectRefusesTerminalTransaction(t *testing.T) {
	transaction := internalTransaction(t, wagering.Bet, "")
	if err := transaction.Reject(wagering.FailureInsufficientFunds, internalNow); err != nil {
		t.Fatalf("Reject devolveu erro: %v", err)
	}

	_, err := internalUseCase().reject(
		context.Background(), noopRepositories{}, transaction,
		wagering.FailureCurrencyMismatch, internalNow)

	if err == nil {
		t.Error("esperava recusa ao rejeitar uma transação já terminal")
	}
}

func TestMarkPendingRefusesNonReversal(t *testing.T) {
	_, err := internalUseCase().markPending(
		context.Background(), noopRepositories{}, internalTransaction(t, wagering.Bet, ""), internalNow)

	if err == nil {
		t.Error("esperava recusa: apenas reversões dependem de referência")
	}
}

// referenceRepositories devolve uma aposta processada como referência, para
// exercitar a resolução de reversão fora do caminho público.
type referenceRepositories struct {
	noopRepositories
	reference *wagering.Transaction
}

func (r referenceRepositories) Transactions() port.TransactionRepository {
	return referenceTransactions{reference: r.reference}
}

type referenceTransactions struct {
	noopTransactions
	reference *wagering.Transaction
}

func (r referenceTransactions) FindByExternalID(context.Context, string, string) (*wagering.Transaction, error) {
	return r.reference, nil
}

func TestApplyReversalRefusesTerminalTransaction(t *testing.T) {
	reference := internalTransaction(t, wagering.Bet, "")
	if err := reference.MarkProcessed(wagering.Result{
		Balance:       internalMoney(t, "975.00"),
		WalletVersion: 2,
	}, internalNow); err != nil {
		t.Fatalf("MarkProcessed devolveu erro: %v", err)
	}

	reversal := internalTransaction(t, wagering.Refund, "transaction-123")
	if err := reversal.Reject(wagering.FailureDuplicateReversal, internalNow); err != nil {
		t.Fatalf("Reject devolveu erro: %v", err)
	}

	parsed := request{
		command:  ProcessTransactionCommand{ProviderID: "provider-a", RoundID: "round-987"},
		kind:     wagering.Refund,
		walletID: reference.WalletID(),
		playerID: reference.PlayerID(),
		amount:   internalMoney(t, "25.00"),
	}

	_, err := internalUseCase().applyReversal(
		context.Background(),
		referenceRepositories{reference: reference},
		internalWallet(t), reversal, parsed, internalNow)

	if err == nil {
		t.Error("esperava recusa ao resolver referência de uma transação terminal")
	}
}

func TestRecordOpeningRefusesWalletWithoutBalance(t *testing.T) {
	empty, err := wallet.Open(
		internalID(t, "0192f291-27dd-7d3f-8071-5f8685deef37"),
		internalID(t, "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"),
		money.Zero(money.BRL), internalNow)
	if err != nil {
		t.Fatalf("Open devolveu erro: %v", err)
	}

	useCase := NewOpenWallet(nil, nil, fixedIDs{})

	if err := useCase.recordOpening(context.Background(), noopRepositories{}, empty); err == nil {
		t.Error("esperava recusa: abertura sem crédito não gera transação")
	}
}
