package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/application/usecase"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
	"github.com/vigmi/backend-challenge-go/internal/platform/auth"
	"github.com/vigmi/backend-challenge-go/internal/platform/health"
)

func TestRouterRejectsAnonymousBusinessRoute(t *testing.T) {
	router := newAuthTestRouter(t, auth.Identity{}, &authTestRepos{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wagering/transactions/"+mustSharedID(t).String(), nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", rec.Code)
	}
}

func TestRouterForbidsProviderOpeningWallet(t *testing.T) {
	router := newAuthTestRouter(t, auth.Identity{
		Role:       auth.RoleProvider,
		ProviderID: "provider-a",
	}, &authTestRepos{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/wallets", nil)
	req.Header.Set("Authorization", "Bearer x")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", rec.Code)
	}
}

func TestRouterForbidsForeignProviderLookup(t *testing.T) {
	tx := mustExternalTransaction(t, "provider-a", "ext-1")
	repos := &authTestRepos{transaction: tx}
	router := newAuthTestRouter(t, auth.Identity{
		Role:       auth.RoleProvider,
		ProviderID: "provider-b",
	}, repos)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/provider-a/wagering/transactions/ext-1", nil)
	req.Header.Set("Authorization", "Bearer x")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func newAuthTestRouter(t *testing.T, identity auth.Identity, repos *authTestRepos) http.Handler {
	t.Helper()
	authenticator := auth.NewAuthenticator(authTestValidator{identity: identity})
	uow := repos
	ids := authTestIDs{id: mustSharedID(t)}
	clock := authTestClock{now: time.Now().UTC()}

	return NewRouter(
		discardLogger(),
		NewHealthHandler(health.NewChecker(nil)),
		authenticator,
		NewWalletHandler(usecase.NewOpenWallet(uow, clock, ids), usecase.NewReconcileWallet(uow), uow, discardLogger(), nil),
		NewWageringHandler(usecase.NewProcessWagerTransaction(uow, clock, ids), uow, discardLogger(), nil),
		nil,
	)
}

type authTestValidator struct {
	identity auth.Identity
}

func (v authTestValidator) Validate(context.Context, string) (auth.Identity, error) {
	if v.identity.Role == "" {
		return auth.Identity{}, auth.ErrUnauthenticated
	}
	return v.identity, nil
}

type authTestClock struct{ now time.Time }

func (c authTestClock) Now() time.Time { return c.now }

type authTestIDs struct{ id shared.ID }

func (g authTestIDs) NewID() (shared.ID, error) { return g.id, nil }

type authTestRepos struct {
	transaction *wagering.Transaction
}

func (r *authTestRepos) Execute(ctx context.Context, fn func(context.Context, port.Repositories) error) error {
	return fn(ctx, r)
}

func (r *authTestRepos) ExecuteReadOnly(ctx context.Context, fn func(context.Context, port.Repositories) error) error {
	return fn(ctx, r)
}

func (r *authTestRepos) Wallets() port.WalletRepository { return authTestWallets{} }
func (r *authTestRepos) Transactions() port.TransactionRepository {
	return authTestTransactions{tx: r.transaction}
}
func (r *authTestRepos) Ledger() port.LedgerRepository { return authTestLedger{} }
func (r *authTestRepos) Inbox() port.InboxRepository   { return authTestInbox{} }
func (r *authTestRepos) Outbox() port.OutboxRepository { return authTestOutbox{} }

type authTestInbox struct{}

func (authTestInbox) Record(context.Context, port.InboxMessage) error { return nil }
func (authTestInbox) Find(context.Context, string, string) (port.InboxMessage, error) {
	return port.InboxMessage{}, port.ErrNotFound
}

type authTestOutbox struct{}

func (authTestOutbox) Append(context.Context, port.OutboxRecord) error { return nil }
func (authTestOutbox) Claim(context.Context, string, int, time.Duration, time.Time) ([]port.OutboxRecord, error) {
	return nil, nil
}
func (authTestOutbox) MarkPublished(context.Context, shared.ID, time.Time) error { return nil }
func (authTestOutbox) ReleaseWithBackoff(context.Context, shared.ID, int, time.Time, time.Time) error {
	return nil
}

type authTestWallets struct{}

func (authTestWallets) Create(context.Context, *wallet.Wallet) error { return port.ErrConflict }
func (authTestWallets) LockByID(context.Context, shared.ID) (*wallet.Wallet, error) {
	return nil, port.ErrNotFound
}
func (authTestWallets) FindByID(context.Context, shared.ID) (*wallet.Wallet, error) {
	return nil, port.ErrNotFound
}
func (authTestWallets) UpdateBalance(context.Context, *wallet.Wallet) error { return nil }

type authTestTransactions struct {
	tx *wagering.Transaction
}

func (r authTestTransactions) Create(context.Context, *wagering.Transaction) error {
	return nil
}
func (r authTestTransactions) Update(context.Context, *wagering.Transaction) error { return nil }
func (r authTestTransactions) FindByID(context.Context, shared.ID) (*wagering.Transaction, error) {
	if r.tx == nil {
		return nil, port.ErrNotFound
	}
	return r.tx, nil
}
func (r authTestTransactions) FindByIdempotencyKey(context.Context, string, string) (*wagering.Transaction, error) {
	return nil, port.ErrNotFound
}
func (r authTestTransactions) FindByExternalID(_ context.Context, providerID, externalID string) (*wagering.Transaction, error) {
	if r.tx == nil || r.tx.ProviderID() != providerID || r.tx.ExternalID() != externalID {
		return nil, port.ErrNotFound
	}
	return r.tx, nil
}
func (r authTestTransactions) HasSuccessfulReversal(context.Context, shared.ID) (bool, error) {
	return false, nil
}
func (r authTestTransactions) ClaimPendingReferences(context.Context, int, time.Time) ([]*wagering.Transaction, error) {
	return nil, nil
}

type authTestLedger struct{}

func (authTestLedger) Append(context.Context, ledger.Entry) error { return nil }
func (authTestLedger) ListByWallet(context.Context, shared.ID, *port.LedgerCursor, int) (port.LedgerPage, error) {
	return port.LedgerPage{}, nil
}
func (authTestLedger) SumByWallet(context.Context, shared.ID) (money.Money, int, error) {
	return money.Zero(money.BRL), 0, nil
}

func mustSharedID(t *testing.T) shared.ID {
	t.Helper()
	id, err := shared.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustExternalTransaction(t *testing.T, providerID, externalID string) *wagering.Transaction {
	t.Helper()
	amount, err := money.Parse("10.00", "BRL")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := wagering.NewExternal(wagering.ExternalParams{
		ID:             mustSharedID(t),
		Kind:           wagering.Bet,
		WalletID:       mustSharedID(t),
		PlayerID:       mustSharedID(t),
		Amount:         amount,
		ProviderID:     providerID,
		ExternalID:     externalID,
		IdempotencyKey: providerID + ":" + externalID,
		PayloadHash:    "hash",
		RoundID:        "round",
		GameID:         "game",
		CreatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkProcessed(wagering.Result{Balance: amount, WalletVersion: 1}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return tx
}
