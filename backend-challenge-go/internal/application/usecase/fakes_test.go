package usecase_test

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// As fakes abaixo servem para exercitar as decisões do caso de uso sem
// infraestrutura. As garantias que dependem do banco — lock, unicidade,
// atomicidade — são provadas nos testes de integração, com PostgreSQL real.

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

type fakeIDs struct {
	next      int
	failAfter int // zero desativa a falha
	calls     int
}

func (g *fakeIDs) NewID() (shared.ID, error) {
	g.calls++
	if g.failAfter > 0 && g.calls >= g.failAfter {
		return shared.ID{}, errors.New("falha ao gerar identificador")
	}
	g.next++
	return shared.ParseID(sequentialUUID(g.next))
}

func sequentialUUID(sequence int) string {
	const digits = "0123456789abcdef"
	suffix := []byte("000000000000")
	value := sequence
	for index := len(suffix) - 1; index >= 0 && value > 0; index-- {
		suffix[index] = digits[value%16]
		value /= 16
	}
	return "0192f2a0-0000-7000-8000-" + string(suffix)
}

// fakeUnitOfWork mantém o estado em memória e reproduz o contrato do port:
// erro na função desfaz as escritas daquela chamada.
type fakeUnitOfWork struct {
	state *fakeState

	beginErr    error
	commitPanic bool
}

type fakeState struct {
	wallets      map[string]walletRecord
	transactions map[string]*wagering.Transaction
	ledger       []ledger.Entry

	walletCreateErr error
	walletLockErr   error
	walletFindErr   error
	walletUpdateErr error
	transactionErr  error
	findByKeyErr    error
	findExternalErr error
	// findExternalErrOn restringe a falha a um id externo específico, para
	// separar a busca de replay da busca da referência de uma reversão.
	findExternalErrOn string
	reversalCheckErr  error
	ledgerAppendErr   error
	ledgerSumErr      error
	ledgerSumMoney    *money.Money
}

type walletRecord struct {
	wallet *wallet.Wallet
}

func newFakeUnitOfWork() *fakeUnitOfWork {
	return &fakeUnitOfWork{state: &fakeState{
		wallets:      map[string]walletRecord{},
		transactions: map[string]*wagering.Transaction{},
	}}
}

func (u *fakeUnitOfWork) Execute(
	ctx context.Context,
	fn func(ctx context.Context, repositories port.Repositories) error,
) error {
	if u.beginErr != nil {
		return u.beginErr
	}

	snapshot := u.state.clone()
	if err := fn(ctx, &fakeRepositories{state: u.state}); err != nil {
		u.state.restore(snapshot)
		return err
	}
	return nil
}

func (u *fakeUnitOfWork) ExecuteReadOnly(
	ctx context.Context,
	fn func(ctx context.Context, repositories port.Repositories) error,
) error {
	if u.beginErr != nil {
		return u.beginErr
	}
	return fn(ctx, &fakeRepositories{state: u.state})
}

func (s *fakeState) clone() *fakeState {
	copied := &fakeState{
		wallets:      make(map[string]walletRecord, len(s.wallets)),
		transactions: make(map[string]*wagering.Transaction, len(s.transactions)),
		ledger:       append([]ledger.Entry(nil), s.ledger...),
	}
	for key, value := range s.wallets {
		copied.wallets[key] = value
	}
	for key, value := range s.transactions {
		copied.transactions[key] = value
	}
	return copied
}

func (s *fakeState) restore(snapshot *fakeState) {
	s.wallets = snapshot.wallets
	s.transactions = snapshot.transactions
	s.ledger = snapshot.ledger
}

type fakeRepositories struct {
	state *fakeState
}

func (r *fakeRepositories) Wallets() port.WalletRepository { return &fakeWallets{state: r.state} }
func (r *fakeRepositories) Transactions() port.TransactionRepository {
	return &fakeTransactions{state: r.state}
}
func (r *fakeRepositories) Ledger() port.LedgerRepository { return &fakeLedger{state: r.state} }

type fakeWallets struct {
	state *fakeState
}

func (r *fakeWallets) Create(_ context.Context, target *wallet.Wallet) error {
	if r.state.walletCreateErr != nil {
		return r.state.walletCreateErr
	}
	for _, existing := range r.state.wallets {
		if existing.wallet.PlayerID().Equal(target.PlayerID()) &&
			existing.wallet.Currency() == target.Currency() {
			return port.ErrConflict.Messagef("carteira duplicada")
		}
	}
	stored, err := cloneWallet(target)
	if err != nil {
		return err
	}
	r.state.wallets[target.ID().String()] = walletRecord{wallet: stored}
	return nil
}

func (r *fakeWallets) LockByID(ctx context.Context, id shared.ID) (*wallet.Wallet, error) {
	if r.state.walletLockErr != nil {
		return nil, r.state.walletLockErr
	}
	return r.FindByID(ctx, id)
}

func (r *fakeWallets) FindByID(_ context.Context, id shared.ID) (*wallet.Wallet, error) {
	if r.state.walletFindErr != nil {
		return nil, r.state.walletFindErr
	}
	record, ok := r.state.wallets[id.String()]
	if !ok {
		return nil, port.ErrNotFound.Messagef("carteira %s", id)
	}
	// Devolve uma cópia, como faz o repositório real ao reidratar a linha:
	// assim uma movimentação desfeita pelo rollback não vaza em memória.
	return cloneWallet(record.wallet)
}

func (r *fakeWallets) UpdateBalance(_ context.Context, target *wallet.Wallet) error {
	if r.state.walletUpdateErr != nil {
		return r.state.walletUpdateErr
	}
	if _, ok := r.state.wallets[target.ID().String()]; !ok {
		return port.ErrNotFound.Messagef("carteira %s", target.ID())
	}
	stored, err := cloneWallet(target)
	if err != nil {
		return err
	}
	r.state.wallets[target.ID().String()] = walletRecord{wallet: stored}
	return nil
}

func cloneWallet(source *wallet.Wallet) (*wallet.Wallet, error) {
	return wallet.Rehydrate(wallet.State{
		ID:        source.ID(),
		PlayerID:  source.PlayerID(),
		Balance:   source.Balance(),
		Version:   source.Version(),
		CreatedAt: source.CreatedAt(),
		UpdatedAt: source.UpdatedAt(),
	})
}

type fakeTransactions struct {
	state *fakeState
}

func (r *fakeTransactions) Create(_ context.Context, target *wagering.Transaction) error {
	if r.state.transactionErr != nil {
		return r.state.transactionErr
	}
	r.state.transactions[target.ID().String()] = target
	return nil
}

func (r *fakeTransactions) Update(_ context.Context, target *wagering.Transaction) error {
	if r.state.transactionErr != nil {
		return r.state.transactionErr
	}
	if _, ok := r.state.transactions[target.ID().String()]; !ok {
		return port.ErrNotFound.Messagef("transação %s", target.ID())
	}
	r.state.transactions[target.ID().String()] = target
	return nil
}

func (r *fakeTransactions) FindByID(_ context.Context, id shared.ID) (*wagering.Transaction, error) {
	found, ok := r.state.transactions[id.String()]
	if !ok {
		return nil, port.ErrNotFound.Messagef("transação %s", id)
	}
	return found, nil
}

func (r *fakeTransactions) FindByIdempotencyKey(
	_ context.Context,
	providerID, idempotencyKey string,
) (*wagering.Transaction, error) {
	if r.state.findByKeyErr != nil {
		return nil, r.state.findByKeyErr
	}
	for _, candidate := range r.sorted() {
		if candidate.ProviderID() == providerID && candidate.IdempotencyKey() == idempotencyKey {
			return candidate, nil
		}
	}
	return nil, port.ErrNotFound.Messagef("chave %s", idempotencyKey)
}

func (r *fakeTransactions) FindByExternalID(
	_ context.Context,
	providerID, externalID string,
) (*wagering.Transaction, error) {
	if r.state.findExternalErr != nil &&
		(r.state.findExternalErrOn == "" || r.state.findExternalErrOn == externalID) {
		return nil, r.state.findExternalErr
	}
	for _, candidate := range r.sorted() {
		if candidate.ProviderID() == providerID && candidate.ExternalID() == externalID {
			return candidate, nil
		}
	}
	return nil, port.ErrNotFound.Messagef("operação %s", externalID)
}

func (r *fakeTransactions) HasSuccessfulReversal(
	_ context.Context,
	referenceID shared.ID,
) (bool, error) {
	if r.state.reversalCheckErr != nil {
		return false, r.state.reversalCheckErr
	}
	for _, candidate := range r.sorted() {
		reference, ok := candidate.ReferenceID()
		if ok && reference.Equal(referenceID) &&
			candidate.Kind().IsReversal() && candidate.Status() == wagering.Processed {
			return true, nil
		}
	}
	return false, nil
}

// sorted torna a busca determinística, independentemente da ordem do map.
func (r *fakeTransactions) sorted() []*wagering.Transaction {
	keys := make([]string, 0, len(r.state.transactions))
	for key := range r.state.transactions {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	sorted := make([]*wagering.Transaction, 0, len(keys))
	for _, key := range keys {
		sorted = append(sorted, r.state.transactions[key])
	}
	return sorted
}

type fakeLedger struct {
	state *fakeState
}

func (r *fakeLedger) Append(_ context.Context, entry ledger.Entry) error {
	if r.state.ledgerAppendErr != nil {
		return r.state.ledgerAppendErr
	}
	for _, existing := range r.state.ledger {
		if existing.WalletID().Equal(entry.WalletID()) &&
			existing.TransactionID().Equal(entry.TransactionID()) {
			return port.ErrConflict.Messagef("lançamento duplicado")
		}
	}
	r.state.ledger = append(r.state.ledger, entry)
	return nil
}

func (r *fakeLedger) ListByWallet(
	_ context.Context,
	walletID shared.ID,
	_ *port.LedgerCursor,
	limit int,
) (port.LedgerPage, error) {
	page := port.LedgerPage{}
	for _, entry := range r.state.ledger {
		if entry.WalletID().Equal(walletID) && len(page.Entries) < limit {
			page.Entries = append(page.Entries, entry)
		}
	}
	return page, nil
}

func (r *fakeLedger) SumByWallet(_ context.Context, walletID shared.ID) (money.Money, int, error) {
	if r.state.ledgerSumErr != nil {
		return money.Money{}, 0, r.state.ledgerSumErr
	}

	var (
		total    money.Money
		entries  int
		currency money.Currency
	)
	for _, entry := range r.state.ledger {
		if !entry.WalletID().Equal(walletID) {
			continue
		}
		entries++
		currency = entry.Amount().Currency()

		signed := entry.Amount()
		if entry.Direction() == ledger.Debit {
			negated, err := signed.Negate()
			if err != nil {
				return money.Money{}, 0, err
			}
			signed = negated
		}
		if total.Currency().IsZero() {
			total = money.Zero(currency)
		}
		sum, err := total.Add(signed)
		if err != nil {
			return money.Money{}, 0, err
		}
		total = sum
	}

	if r.state.ledgerSumMoney != nil {
		return *r.state.ledgerSumMoney, entries, nil
	}
	return total, entries, nil
}
