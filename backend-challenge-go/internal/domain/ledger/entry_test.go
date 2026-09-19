package ledger_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/ledger"
	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

var (
	entryID       = mustID("0192f298-345e-7e38-af88-e43f851a819d")
	walletID      = mustID("0192f291-27dd-7d3f-8071-5f8685deef37")
	transactionID = mustID("0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1")
	createdAt     = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
)

func mustID(raw string) shared.ID {
	parsed, err := shared.ParseID(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

func brl(t *testing.T, amount string) money.Money {
	t.Helper()
	parsed, err := money.Parse(amount, "BRL")
	if err != nil {
		t.Fatalf("Parse(%q) devolveu erro: %v", amount, err)
	}
	return parsed
}

func TestParseDirection(t *testing.T) {
	tests := []struct {
		raw  string
		want ledger.Direction
	}{
		{"DEBIT", ledger.Debit},
		{"CREDIT", ledger.Credit},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			parsed, err := ledger.ParseDirection(tt.raw)
			if err != nil {
				t.Fatalf("ParseDirection devolveu erro: %v", err)
			}
			if parsed != tt.want {
				t.Errorf("ParseDirection = %q, esperado %q", parsed, tt.want)
			}
			if parsed.String() != tt.raw {
				t.Errorf("String = %q, esperado %q", parsed.String(), tt.raw)
			}
		})
	}
}

func TestParseDirectionRejectsUnknownValues(t *testing.T) {
	for _, raw := range []string{"", "debit", "CRÉDITO", "TRANSFER"} {
		t.Run(raw, func(t *testing.T) {
			_, err := ledger.ParseDirection(raw)

			if !errors.Is(err, ledger.ErrInvalidDirection) {
				t.Errorf("erro = %v, esperado ErrInvalidDirection", err)
			}
		})
	}
}

func TestNewEntryAcceptsConsistentMovements(t *testing.T) {
	tests := []struct {
		name      string
		direction ledger.Direction
		amount    string
		before    string
		after     string
	}{
		{"débito", ledger.Debit, "25.00", "1000.00", "975.00"},
		{"crédito", ledger.Credit, "25.00", "975.00", "1000.00"},
		{"débito até zero", ledger.Debit, "100.00", "100.00", "0.00"},
		{"crédito de abertura", ledger.Credit, "1000.00", "0.00", "1000.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := ledger.NewEntry(entryID, walletID, transactionID, tt.direction,
				brl(t, tt.amount), brl(t, tt.before), brl(t, tt.after), createdAt)
			if err != nil {
				t.Fatalf("NewEntry devolveu erro: %v", err)
			}

			if !entry.ID().Equal(entryID) || !entry.WalletID().Equal(walletID) ||
				!entry.TransactionID().Equal(transactionID) {
				t.Errorf("identidade incorreta: %+v", entry)
			}
			if entry.Direction() != tt.direction {
				t.Errorf("Direction = %q, esperado %q", entry.Direction(), tt.direction)
			}
			if entry.Amount().String() != tt.amount {
				t.Errorf("Amount = %s, esperado %s", entry.Amount(), tt.amount)
			}
			if entry.BalanceBefore().String() != tt.before || entry.BalanceAfter().String() != tt.after {
				t.Errorf("saldos = %s → %s, esperado %s → %s",
					entry.BalanceBefore(), entry.BalanceAfter(), tt.before, tt.after)
			}
			if !entry.CreatedAt().Equal(createdAt) {
				t.Errorf("CreatedAt = %s, esperado %s", entry.CreatedAt(), createdAt)
			}
		})
	}
}

func TestNewEntryNormalizesTimestampToUTC(t *testing.T) {
	saoPaulo := time.FixedZone("America/Sao_Paulo", -3*60*60)
	local := time.Date(2026, 9, 8, 9, 0, 0, 0, saoPaulo)

	entry, err := ledger.NewEntry(entryID, walletID, transactionID, ledger.Debit,
		brl(t, "25.00"), brl(t, "1000.00"), brl(t, "975.00"), local)
	if err != nil {
		t.Fatalf("NewEntry devolveu erro: %v", err)
	}

	if entry.CreatedAt().Location() != time.UTC {
		t.Errorf("Location = %s, esperado UTC", entry.CreatedAt().Location())
	}
}

// A equação balanceAfter = balanceBefore ± amount é a proteção contra
// lançamento que não corresponde à movimentação efetivamente aplicada.
func TestNewEntryRejectsBrokenBalanceEquation(t *testing.T) {
	tests := []struct {
		name      string
		direction ledger.Direction
		amount    string
		before    string
		after     string
	}{
		{"débito com saldo posterior maior", ledger.Debit, "25.00", "1000.00", "1025.00"},
		{"crédito com saldo posterior menor", ledger.Credit, "25.00", "1000.00", "975.00"},
		{"débito com diferença errada", ledger.Debit, "25.00", "1000.00", "980.00"},
		{"crédito sem alterar saldo", ledger.Credit, "25.00", "1000.00", "1000.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ledger.NewEntry(entryID, walletID, transactionID, tt.direction,
				brl(t, tt.amount), brl(t, tt.before), brl(t, tt.after), createdAt)

			if !errors.Is(err, ledger.ErrInconsistentEntry) {
				t.Errorf("erro = %v, esperado ErrInconsistentEntry", err)
			}
		})
	}
}

func TestNewEntryRejectsInvalidInput(t *testing.T) {
	var zeroID shared.ID
	var uninitialized money.Money
	usd, err := money.Parse("25.00", "USD")
	if err != nil {
		t.Fatalf("Parse devolveu erro: %v", err)
	}

	type entryArgs struct {
		id, wallet, transaction shared.ID
		direction               ledger.Direction
		amount, before, after   money.Money
		createdAt               time.Time
	}
	valid := entryArgs{
		id: entryID, wallet: walletID, transaction: transactionID,
		direction: ledger.Debit,
		amount:    brl(t, "25.00"), before: brl(t, "1000.00"), after: brl(t, "975.00"),
		createdAt: createdAt,
	}

	tests := []struct {
		name     string
		mutate   func(*entryArgs)
		wantCode error
	}{
		{"sem id", func(a *entryArgs) { a.id = zeroID }, shared.ErrInvalidID},
		{"sem carteira", func(a *entryArgs) { a.wallet = zeroID }, shared.ErrInvalidID},
		{"sem transação", func(a *entryArgs) { a.transaction = zeroID }, shared.ErrInvalidID},
		{"direção desconhecida", func(a *entryArgs) { a.direction = "TRANSFER" }, ledger.ErrInvalidDirection},
		{"valor não inicializado", func(a *entryArgs) { a.amount = uninitialized }, money.ErrUninitialized},
		{"saldo anterior não inicializado", func(a *entryArgs) { a.before = uninitialized }, money.ErrUninitialized},
		{"saldo posterior não inicializado", func(a *entryArgs) { a.after = uninitialized }, money.ErrUninitialized},
		{"valor zero", func(a *entryArgs) { a.amount = brl(t, "0.00"); a.after = brl(t, "1000.00") }, ledger.ErrInconsistentEntry},
		{"valor negativo", func(a *entryArgs) {
			a.amount = money.FromMinorUnits(-2500, money.BRL)
			a.after = brl(t, "1025.00")
		}, ledger.ErrInconsistentEntry},
		{"moeda divergente", func(a *entryArgs) { a.amount = usd }, money.ErrCurrencyMismatch},
		{"sem instante", func(a *entryArgs) { a.createdAt = time.Time{} }, ledger.ErrInconsistentEntry},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := valid
			tt.mutate(&args)

			_, err := ledger.NewEntry(args.id, args.wallet, args.transaction, args.direction,
				args.amount, args.before, args.after, args.createdAt)

			if !errors.Is(err, tt.wantCode) {
				t.Errorf("erro = %v, esperado %v", err, tt.wantCode)
			}
		})
	}
}

func TestNewEntryRejectsNegativeResultingBalance(t *testing.T) {
	_, err := ledger.NewEntry(entryID, walletID, transactionID, ledger.Debit,
		brl(t, "25.00"), brl(t, "10.00"), money.FromMinorUnits(-1500, money.BRL), createdAt)

	if !errors.Is(err, ledger.ErrInconsistentEntry) {
		t.Errorf("erro = %v, esperado ErrInconsistentEntry", err)
	}
}

func TestNewEntryPropagatesOverflowFromBalanceEquation(t *testing.T) {
	_, err := ledger.NewEntry(entryID, walletID, transactionID, ledger.Credit,
		money.FromMinorUnits(1, money.BRL),
		money.FromMinorUnits(math.MaxInt64, money.BRL),
		money.FromMinorUnits(math.MaxInt64, money.BRL),
		createdAt)

	if !errors.Is(err, money.ErrOverflow) {
		t.Errorf("erro = %v, esperado ErrOverflow", err)
	}
}
