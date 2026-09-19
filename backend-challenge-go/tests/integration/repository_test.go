//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

func TestWalletRepositoryRoundTrip(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	original := openWallet(t, ctx, "1000.00")

	restored := readWallet(t, ctx, original.ID())

	if !restored.ID().Equal(original.ID()) || !restored.PlayerID().Equal(original.PlayerID()) {
		t.Errorf("identidade divergente: %s / %s", restored.ID(), restored.PlayerID())
	}
	if restored.Balance().String() != "1000.00" || restored.Currency() != money.BRL {
		t.Errorf("saldo = %s %s", restored.Balance(), restored.Currency())
	}
	if restored.Version() != original.Version() {
		t.Errorf("versão = %d, esperada %d", restored.Version(), original.Version())
	}
	// Os instantes voltam em UTC, independentemente do fuso da sessão.
	if restored.CreatedAt().Location() != time.UTC {
		t.Errorf("CreatedAt em %s, esperado UTC", restored.CreatedAt().Location())
	}
}

func TestWalletRepositoryReportsNotFound(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	_, err := newUnitOfWork().ReadOnly().Wallets().FindByID(ctx, newID(t))

	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("erro = %v, esperado ErrNotFound", err)
	}
}

// A unicidade de (jogador, moeda) vira conflito de aplicação, sem vazar o
// SQLSTATE para os casos de uso.
func TestWalletRepositoryTranslatesUniqueViolation(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	existing := openWallet(t, ctx, "1000.00")

	duplicate, err := wallet.Open(newID(t), existing.PlayerID(), brl(t, "500.00"), time.Now().UTC())
	if err != nil {
		t.Fatalf("Open devolveu erro: %v", err)
	}

	err = newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		return repositories.Wallets().Create(ctx, duplicate)
	})

	if !errors.Is(err, port.ErrConflict) {
		t.Errorf("erro = %v, esperado ErrConflict", err)
	}
}

func TestWalletRepositoryUpdateRequiresExistingRow(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	orphan, err := wallet.Open(newID(t), newID(t), brl(t, "10.00"), time.Now().UTC())
	if err != nil {
		t.Fatalf("Open devolveu erro: %v", err)
	}

	err = newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		return repositories.Wallets().UpdateBalance(ctx, orphan)
	})

	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("erro = %v, esperado ErrNotFound", err)
	}
}

// Um erro no meio da operação desfaz tudo: sem isso, um débito poderia ficar
// sem o lançamento correspondente no ledger.
func TestUnitOfWorkRollsBackEverythingOnError(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "100.00")
	bet := newBet(t, target, "25.00")

	failure := errors.New("falha simulada depois das escritas")

	err := newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		locked, err := repositories.Wallets().LockByID(ctx, target.ID())
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		movement, err := locked.Debit(bet.Amount(), now)
		if err != nil {
			return err
		}
		if err := bet.MarkProcessed(wagering.Result{
			Balance:       movement.BalanceAfter,
			WalletVersion: movement.Version,
		}, now); err != nil {
			return err
		}
		if err := repositories.Transactions().Create(ctx, bet); err != nil {
			return err
		}
		if err := repositories.Wallets().UpdateBalance(ctx, locked); err != nil {
			return err
		}
		return failure
	})

	if !errors.Is(err, failure) {
		t.Fatalf("erro = %v, esperado o erro simulado", err)
	}

	if balance := readWallet(t, ctx, target.ID()).Balance().String(); balance != "100.00" {
		t.Errorf("saldo = %s, esperado 100.00 (a transação deveria ter sido desfeita)", balance)
	}

	_, err = newUnitOfWork().ReadOnly().Transactions().FindByID(ctx, bet.ID())
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("a transação não deveria ter sido persistida: %v", err)
	}
}

func TestUnitOfWorkCommitsEverythingTogether(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "1000.00")
	bet := newBet(t, target, "25.00")

	if err := applyBet(ctx, target.ID(), bet); err != nil {
		t.Fatalf("applyBet devolveu erro: %v", err)
	}

	if balance := readWallet(t, ctx, target.ID()).Balance().String(); balance != "975.00" {
		t.Errorf("saldo = %s, esperado 975.00", balance)
	}

	stored, err := newUnitOfWork().ReadOnly().Transactions().FindByID(ctx, bet.ID())
	if err != nil {
		t.Fatalf("não foi possível ler a transação: %v", err)
	}
	if stored.Status() != wagering.Processed {
		t.Errorf("status = %q, esperado PROCESSED", stored.Status())
	}

	// O resultado congelado é o que sustenta o replay com o saldo da época.
	result, ok := stored.Result()
	if !ok {
		t.Fatal("esperava resultado persistido")
	}
	if result.Balance.String() != "975.00" || result.WalletVersion != 2 {
		t.Errorf("resultado = %s / versão %d", result.Balance, result.WalletVersion)
	}

	if entries := countLedgerEntries(t, ctx, target.ID()); entries != 1 {
		t.Errorf("lançamentos = %d, esperado 1", entries)
	}
}

func TestTransactionRepositoryRoundTripPreservesMetadata(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "1000.00")
	bet := newBet(t, target, "25.00")

	if err := applyBet(ctx, target.ID(), bet); err != nil {
		t.Fatalf("applyBet devolveu erro: %v", err)
	}

	stored, err := newUnitOfWork().ReadOnly().Transactions().
		FindByIdempotencyKey(ctx, "provider-a", bet.IdempotencyKey())
	if err != nil {
		t.Fatalf("consulta por chave devolveu erro: %v", err)
	}

	if !stored.ID().Equal(bet.ID()) {
		t.Errorf("id = %s, esperado %s", stored.ID(), bet.ID())
	}
	if stored.Origin() != wagering.OriginExternal || stored.Kind() != wagering.Bet {
		t.Errorf("origem/tipo = %s / %s", stored.Origin(), stored.Kind())
	}
	if stored.PayloadHash() != bet.PayloadHash() || stored.RoundID() != "round-987" ||
		stored.GameID() != "fortune-chimp" {
		t.Errorf("metadados externos divergentes: %+v", stored)
	}

	byExternal, err := newUnitOfWork().ReadOnly().Transactions().
		FindByExternalID(ctx, "provider-a", bet.ExternalID())
	if err != nil {
		t.Fatalf("consulta por id externo devolveu erro: %v", err)
	}
	if !byExternal.ID().Equal(bet.ID()) {
		t.Errorf("id = %s, esperado %s", byExternal.ID(), bet.ID())
	}
}

// A abertura interna é gravada sem metadados de provedor e volta com a mesma
// separação de origem imposta pelo schema.
func TestTransactionRepositoryHandlesInternalOpening(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "1000.00")

	opening, err := wagering.NewOpening(wagering.OpeningParams{
		ID:        newID(t),
		WalletID:  target.ID(),
		PlayerID:  target.PlayerID(),
		Amount:    target.Balance(),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("NewOpening devolveu erro: %v", err)
	}
	if err := opening.MarkProcessed(wagering.Result{
		Balance:       target.Balance(),
		WalletVersion: target.Version(),
	}, time.Now().UTC()); err != nil {
		t.Fatalf("MarkProcessed devolveu erro: %v", err)
	}

	err = newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		return repositories.Transactions().Create(ctx, opening)
	})
	if err != nil {
		t.Fatalf("não foi possível persistir a abertura: %v", err)
	}

	stored, err := newUnitOfWork().ReadOnly().Transactions().FindByID(ctx, opening.ID())
	if err != nil {
		t.Fatalf("não foi possível ler a abertura: %v", err)
	}

	if stored.Origin() != wagering.OriginInternal || stored.Kind() != wagering.Opening {
		t.Errorf("origem/tipo = %s / %s", stored.Origin(), stored.Kind())
	}
	if stored.ProviderID() != "" || stored.IdempotencyKey() != "" || stored.RoundID() != "" {
		t.Error("a abertura interna não deveria carregar metadados externos")
	}
}

func TestTransactionRepositoryUpdatesStateAndFailureCode(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "10.00")
	bet := newBet(t, target, "80.00")

	err := newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		return repositories.Transactions().Create(ctx, bet)
	})
	if err != nil {
		t.Fatalf("não foi possível persistir a transação: %v", err)
	}

	if err := bet.Reject(wagering.FailureInsufficientFunds, time.Now().UTC()); err != nil {
		t.Fatalf("Reject devolveu erro: %v", err)
	}

	err = newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		return repositories.Transactions().Update(ctx, bet)
	})
	if err != nil {
		t.Fatalf("não foi possível atualizar a transação: %v", err)
	}

	stored, err := newUnitOfWork().ReadOnly().Transactions().FindByID(ctx, bet.ID())
	if err != nil {
		t.Fatalf("não foi possível ler a transação: %v", err)
	}
	if stored.Status() != wagering.Rejected {
		t.Errorf("status = %q, esperado REJECTED", stored.Status())
	}
	if stored.FailureCode() != wagering.FailureInsufficientFunds {
		t.Errorf("failureCode = %q", stored.FailureCode())
	}
	if _, ok := stored.Result(); ok {
		t.Error("uma rejeição não deveria carregar resultado financeiro")
	}
}

func TestLedgerRepositoryPaginatesWithStableCursor(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "1000.00")

	const total = 5
	for index := 0; index < total; index++ {
		if err := applyBet(ctx, target.ID(), newBet(t, target, "10.00")); err != nil {
			t.Fatalf("applyBet devolveu erro: %v", err)
		}
	}

	repositories := newUnitOfWork().ReadOnly()

	first, err := repositories.Ledger().ListByWallet(ctx, target.ID(), nil, 2)
	if err != nil {
		t.Fatalf("primeira página devolveu erro: %v", err)
	}
	if len(first.Entries) != 2 || first.Next == nil {
		t.Fatalf("primeira página = %d lançamentos, próximo cursor %v", len(first.Entries), first.Next)
	}

	second, err := repositories.Ledger().ListByWallet(ctx, target.ID(), first.Next, 2)
	if err != nil {
		t.Fatalf("segunda página devolveu erro: %v", err)
	}
	if len(second.Entries) != 2 {
		t.Fatalf("segunda página = %d lançamentos, esperados 2", len(second.Entries))
	}

	last, err := repositories.Ledger().ListByWallet(ctx, target.ID(), second.Next, 2)
	if err != nil {
		t.Fatalf("última página devolveu erro: %v", err)
	}
	if len(last.Entries) != 1 {
		t.Errorf("última página = %d lançamentos, esperado 1", len(last.Entries))
	}
	if last.Next != nil {
		t.Error("não deveria haver página após a última")
	}

	// Nenhum lançamento pode se repetir entre páginas nem sumir.
	seen := make(map[string]bool)
	for _, page := range [][]ledger.Entry{first.Entries, second.Entries, last.Entries} {
		for _, entry := range page {
			if seen[entry.ID().String()] {
				t.Errorf("lançamento %s apareceu em mais de uma página", entry.ID())
			}
			seen[entry.ID().String()] = true
		}
	}
	if len(seen) != total {
		t.Errorf("lançamentos distintos = %d, esperados %d", len(seen), total)
	}
}

func TestLedgerRepositoryRejectsNonPositiveLimit(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "100.00")

	_, err := newUnitOfWork().ReadOnly().Ledger().ListByWallet(ctx, target.ID(), nil, 0)

	if err == nil {
		t.Fatal("esperava erro para limite não positivo")
	}
}

// A reconciliação reconstrói o saldo a partir do ledger; sem lançamentos, não
// há moeda a inferir e o chamador usa a da carteira.
func TestLedgerRepositorySumsCreditsAndDebits(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "1000.00")

	empty, entries, err := newUnitOfWork().ReadOnly().Ledger().SumByWallet(ctx, target.ID())
	if err != nil {
		t.Fatalf("soma devolveu erro: %v", err)
	}
	if entries != 0 {
		t.Errorf("lançamentos = %d, esperado 0", entries)
	}
	if err := empty.Validate(); err == nil {
		t.Error("sem lançamentos, o valor devolvido não deveria carregar moeda")
	}

	if err := applyBet(ctx, target.ID(), newBet(t, target, "25.00")); err != nil {
		t.Fatalf("applyBet devolveu erro: %v", err)
	}

	sum, entries, err := newUnitOfWork().ReadOnly().Ledger().SumByWallet(ctx, target.ID())
	if err != nil {
		t.Fatalf("soma devolveu erro: %v", err)
	}
	if entries != 1 {
		t.Errorf("lançamentos = %d, esperado 1", entries)
	}
	if sum.String() != "-25.00" {
		t.Errorf("soma = %s, esperado -25.00 (apenas o débito, sem a abertura)", sum)
	}
}

// Barreira final contra movimentação duplicada, agora pela via do repositório.
func TestLedgerRepositoryRejectsDuplicateEntryForTransaction(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "1000.00")
	bet := newBet(t, target, "25.00")

	if err := applyBet(ctx, target.ID(), bet); err != nil {
		t.Fatalf("applyBet devolveu erro: %v", err)
	}

	err := newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
		entry, err := ledger.NewEntry(newID(t), target.ID(), bet.ID(), ledger.Debit,
			brl(t, "25.00"), brl(t, "975.00"), brl(t, "950.00"), time.Now().UTC())
		if err != nil {
			return err
		}
		return repositories.Ledger().Append(ctx, entry)
	})

	if !errors.Is(err, port.ErrConflict) {
		t.Errorf("erro = %v, esperado ErrConflict", err)
	}
}
