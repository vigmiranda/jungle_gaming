//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/wagering"
	"github.com/vigmi/backend-challenge-go/internal/domain/wallet"
)

// Cenário obrigatório do desafio, no nível de persistência: uma carteira com
// 100.00 recebe duas apostas simultâneas de 80.00. O `FOR UPDATE` serializa os
// escritores, então a segunda enxerga o saldo já debitado e é recusada.
func TestConcurrentBetsOnSameWalletDoNotLoseUpdates(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "100.00")

	first := newBet(t, target, "80.00")
	second := newBet(t, target, "80.00")

	bets := []*wagering.Transaction{first, second}
	results := make([]error, len(bets))
	start := make(chan struct{})

	var waitGroup sync.WaitGroup
	waitGroup.Add(len(bets))
	for index, bet := range bets {
		go func(index int, bet *wagering.Transaction) {
			defer waitGroup.Done()
			<-start
			results[index] = applyBet(ctx, target.ID(), bet)
		}(index, bet)
	}
	// As duas goroutines disputam a carteira a partir do mesmo instante.
	close(start)
	waitGroup.Wait()

	var processed, rejected int
	for _, err := range results {
		switch {
		case err == nil:
			processed++
		case errors.Is(err, wallet.ErrInsufficientFunds):
			rejected++
		default:
			t.Fatalf("erro inesperado: %v", err)
		}
	}

	if processed != 1 || rejected != 1 {
		t.Fatalf("resultados = %d processadas e %d rejeitadas, esperado 1 e 1", processed, rejected)
	}

	final := readWallet(t, ctx, target.ID())
	if final.Balance().String() != "20.00" {
		t.Errorf("saldo final = %s, esperado 20.00", final.Balance())
	}
	if final.Version() != 2 {
		t.Errorf("versão final = %d, esperada 2", final.Version())
	}
	if entries := countLedgerEntries(t, ctx, target.ID()); entries != 1 {
		t.Errorf("lançamentos = %d, esperado 1", entries)
	}
}

// Carteiras independentes avançam em paralelo: o lock é por linha, não global.
func TestWalletsAdvanceInParallel(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)

	blocked := openWallet(t, ctx, "100.00")
	independent := openWallet(t, ctx, "100.00")

	// Segura o lock da primeira carteira e mantém a transação aberta.
	holding, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("não foi possível abrir a transação: %v", err)
	}
	defer func() { _ = holding.Rollback(ctx) }()

	if _, err := holding.Exec(ctx,
		`SELECT id FROM wallets WHERE id = $1 FOR UPDATE`, blocked.ID().String()); err != nil {
		t.Fatalf("não foi possível bloquear a carteira: %v", err)
	}

	// A operação na outra carteira não pode esperar pelo lock alheio.
	// A aposta é montada antes de iniciar a goroutine: helpers que chamam
	// t.Fatalf só podem rodar na goroutine do teste.
	bet := newBet(t, independent, "80.00")
	done := make(chan error, 1)
	go func() {
		done <- applyBet(ctx, independent.ID(), bet)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a operação na carteira independente falhou: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a carteira independente ficou bloqueada: o lock não deveria ser global")
	}

	if balance := readWallet(t, ctx, independent.ID()).Balance().String(); balance != "20.00" {
		t.Errorf("saldo da carteira independente = %s, esperado 20.00", balance)
	}
}

// Enquanto uma transação segura a carteira, a outra espera em vez de ler um
// saldo desatualizado.
func TestLockByIDSerializesWriters(t *testing.T) {
	ctx := testContext(t)
	truncateAll(t)
	target := openWallet(t, ctx, "100.00")

	firstLocked := make(chan struct{})
	release := make(chan struct{})
	secondSaw := make(chan string, 1)
	debit := brl(t, "30.00")

	go func() {
		_ = newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
			locked, err := repositories.Wallets().LockByID(ctx, target.ID())
			if err != nil {
				return err
			}
			if _, err := locked.Debit(debit, time.Now().UTC()); err != nil {
				return err
			}
			if err := repositories.Wallets().UpdateBalance(ctx, locked); err != nil {
				return err
			}

			close(firstLocked)
			<-release
			return nil
		})
	}()

	<-firstLocked

	go func() {
		_ = newUnitOfWork().Execute(ctx, func(ctx context.Context, repositories port.Repositories) error {
			locked, err := repositories.Wallets().LockByID(ctx, target.ID())
			if err != nil {
				return err
			}
			secondSaw <- locked.Balance().String()
			return nil
		})
	}()

	// O segundo escritor não pode passar antes do commit do primeiro.
	select {
	case balance := <-secondSaw:
		t.Fatalf("o segundo escritor leu %s sem esperar o lock", balance)
	case <-time.After(300 * time.Millisecond):
	}

	close(release)

	select {
	case balance := <-secondSaw:
		if balance != "70.00" {
			t.Errorf("segundo escritor leu %s, esperado 70.00 (saldo já debitado)", balance)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("o segundo escritor não obteve o lock após o commit")
	}
}
